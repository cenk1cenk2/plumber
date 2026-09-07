package plumber

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cenk1cenk2/plumber/v6/logger"
	"github.com/creasty/defaults"
	validator "github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

type Plumber struct {
	Cli         *cli.Command
	Log         *logrus.Logger
	Environment AppEnvironment
	Terminator
	Validator *validator.Validate

	context context.Context
	cancel  context.CancelCauseFunc

	secrets       []string
	onTerminateFn PlumberOnTerminateFn
	exitFn        PlumberExitFn
	exitOnce      *sync.Once
	options       PlumberOptions
	runtime       Runtime
}

// ErrShutdown is the cause the root context of the application is cancelled with whenever plumber
// shuts itself down, therefore a flow that is interrupted by the shutdown can be told apart from a
// flow that is cancelled by whoever runs it.
var ErrShutdown = errors.New("Application is shutting down.")

type PlumberOptions struct {
	delimiter          string
	documentation      DocumentationOptions
	deprecationNotices []DeprecationNotice
	timeout            time.Duration
	greeter            PlumberFn
}

type AppEnvironment struct {
	Debug bool
	CI    bool
}

type Terminator struct {
	Enabled bool

	state *terminatorState
}

/*
Holds the components that are registered to the terminator of the application.

While the application is running the components register themselves as they start running and
release themselves again as they are done, and while the application is terminating the hooks of the
components that are still running are drained, which is what the termination of the application
waits for.
*/
type terminatorState struct {
	lock sync.Mutex
	// components that are registered with a hook, by the handle they are registered with
	hooks map[int]terminatorHookFn
	// components that are registered through the public API without a hook
	anonymous int
	// hooks that are currently running while the application is terminating
	draining  int
	handle    int
	initiated bool
	drained   chan struct{}
	closed    bool
}

type terminatorHookFn func(ctx context.Context)

type DocumentationOptions struct {
	MarkdownOutputFile          string
	EmbeddedMarkdownOutputFile  string
	MarkdownBehead              int
	ExcludeFlags                bool
	ExcludeEnvironmentVariables bool
	IncludeDefaultCommands      bool
	IncludeDefaultFlags         bool
}

type DeprecationNotice struct {
	Message     string
	Environment []string
	Flag        []string
	Level       LogLevel
}

type (
	PlumberOnTerminateFn func() error
	PlumberNewFn         func(p *Plumber) *cli.Command
	PlumberFn            func(p *Plumber) error
	PlumberPredicate     func(p *Plumber) bool
	PlumberExitFn        func(code int)
)

const (
	log_status_plumber_terminator  string = "terminate"
	log_status_plumber_parser      string = "parse"
	log_status_plumber_environment string = "env"
	log_status_plumber_setup       string = "setup"
)

// Creates a new Plumber instance and initiates it.
func NewPlumber(fn PlumberNewFn) *Plumber {
	p := &Plumber{}

	p.context, p.cancel = context.WithCancelCause(context.Background())

	p.Cli = fn(p)

	p.exitFn = os.Exit
	p.exitOnce = &sync.Once{}

	p.Terminator = Terminator{
		Enabled: false,
	}

	p.options = PlumberOptions{
		delimiter: ":",
		timeout:   time.Second * 5,
		greeter:   greeter,
	}
	p.runtime = Runtime{}

	p.Validator = validator.New()

	p.Cli.Before = p.setup(p.Cli.Before)

	p.Cli.Flags = p.appendDefaultFlags(p.Cli.Flags)

	p.Environment = AppEnvironment{}

	// presetup logger to not have it nil in edge cases
	p.Log = logger.InitiateLogger(logrus.InfoLevel)
	formatter := &logger.Formatter{
		FieldsOrder:      []string{LOG_FIELD_CONTEXT, LOG_FIELD_STATUS},
		TimestampFormat:  "",
		HideKeys:         true,
		NoColors:         false,
		NoFieldsColors:   false,
		NoFieldsSpace:    false,
		NoEmptyFields:    true,
		ShowFullLevel:    false,
		NoUppercaseLevel: false,
		TrimMessages:     true,
		CallerFirst:      true,
		Secrets:          &p.secrets,
	}
	p.SetFormatter(formatter)

	p.registerInterruptHandler()

	return p
}

// Sets additional configuration fields.
func (p *Plumber) Set(fn PlumberFn) *Plumber {
	if err := fn(p); err != nil {
		p.SendFatal(nil, err)
	}

	return p
}

// Sets documentation options of the application.
func (p *Plumber) SetDocumentationOptions(options DocumentationOptions) *Plumber {
	p.options.documentation = options

	return p
}

// Sets delimiter for the application.
func (p *Plumber) SetDelimiter(delimiter string) *Plumber {
	p.options.delimiter = delimiter

	return p
}

// Sets timeout for terminator of the application.
func (p *Plumber) SetTerminatorTimeout(timeout time.Duration) *Plumber {
	p.options.timeout = timeout

	return p
}

// Sets the deprecation notices for the application.
func (p *Plumber) SetDeprecationNotices(notices ...[]DeprecationNotice) *Plumber {
	for _, notice := range notices {
		p.options.deprecationNotices = append(p.options.deprecationNotices, notice...)
	}

	return p
}

// Sets the greeter function to print out the CLI name and version.
func (p *Plumber) SetGreeter(fn PlumberFn) *Plumber {
	p.options.greeter = fn

	return p
}

// Disables the greeter function to print out the CLI name and version.
func (p *Plumber) DisableGreeter() *Plumber {
	p.options.greeter = nil

	return p
}

func (p *Plumber) SetRuntime(runtime Runtime) *Plumber {
	p.runtime = runtime

	return p
}

func (p *Plumber) RunWith(runtime Runtime, fn PlumberFn) error {
	if fn == nil {
		return fmt.Errorf("runtime callback must be set")
	}

	scoped := *p
	scoped.runtime = runtime.inherit(p.runtime)

	return fn(&scoped)
}

/*
Enables terminator globally for the current application.

If terminator functions are going to be used inside task lists, tasks and commands, terminator should be globally enabled.
The terminate information will be propagated through the channels to the subcomponents.
*/
func (p *Plumber) EnableTerminator() *Plumber {
	p.Terminator = Terminator{
		Enabled: true,
		state: &terminatorState{
			hooks:   map[int]terminatorHookFn{},
			drained: make(chan struct{}),
		},
	}

	p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_plumber_terminator,
	}).Traceln("Terminator enabled.")

	return p
}

// Sets the action that would be executed on terminate.
func (p *Plumber) SetOnTerminate(fn PlumberOnTerminateFn) *Plumber {
	p.onTerminateFn = fn

	return p
}

// Sets the function that ends the process whenever the application exits, which is os.Exit itself
// unless it is overwritten.
func (p *Plumber) SetExitFunc(fn PlumberExitFn) *Plumber {
	if fn == nil {
		fn = os.Exit
	}

	p.exitFn = fn

	return p
}

// sets formatter for the plumber.
func (p *Plumber) SetFormatter(formatter *logger.Formatter) *Plumber {
	p.Log.SetFormatter(formatter)

	return p
}

// Adds sensitive information so that the logger will not log out the given secrets.
func (p *Plumber) AppendSecrets(secrets ...string) *Plumber {
	p.secrets = append(p.secrets, secrets...)

	return p
}

// Logs an error with its custom instance of logger.
func (p *Plumber) SendError(log *logrus.Entry, err error) *Plumber {
	if err == nil {
		return p
	}

	if log == nil {
		log = p.Log.WithFields(logrus.Fields{})
	}

	log.Errorln(err)

	return p
}

// Logs a fatal error with its custom instance of logger and exits the application with code 1.
func (p *Plumber) SendFatal(log *logrus.Entry, err error) *Plumber {
	p.SendError(log, err)

	p.exit(fmt.Sprintf("Fatal error has been received: %v", err), 1)

	return p
}

// Sends exit code to terminate the application.
func (p *Plumber) SendExit(code int) *Plumber {
	p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_exit,
	}).Traceln(code)

	p.exit(fmt.Sprintf("Will exit with code: %d", code), code)

	return p
}

// Sends a terminate request to the application via interruption signal.
func (p *Plumber) SendTerminate(sig os.Signal, code int) {
	if p.Terminator.Enabled {
		log := p.Log.WithFields(logrus.Fields{
			LOG_FIELD_CONTEXT: p.Cli.Name,
			LOG_FIELD_STATUS:  log_status_plumber_terminator,
		})

		if p.Terminator.state.isInitiated() {
			log.Tracef("Termination process already started, ignoring: %s", sig)

			return
		}

		log.Tracef("Sending should terminate through terminator: %s", sig)
	}

	p.Terminate(code)
}

/*
Sends a terminate request through the application.

This will gracefully try to stop the application components that are registered and listening for the terminator.
*/
func (p *Plumber) Terminate(code int) {
	p.exit(fmt.Sprintf("Terminating with code: %d", code), code)
}

// Registers a new component that should be handled by the terminator.
func (p *Plumber) RegisterTerminator() *Plumber {
	if !p.ensureTerminator() {
		return p
	}

	p.Terminator.state.register(nil)

	return p
}

func (p *Plumber) DeregisterTerminator() *Plumber {
	if !p.ensureTerminator() {
		return p
	}

	p.Terminator.state.release(0)

	return p
}

// Register a component as successfully terminated.
func (p *Plumber) RegisterTerminated() *Plumber {
	if !p.ensureTerminator() {
		return p
	}

	p.Terminator.state.release(0)

	return p
}

// Returns the channel that is closed as soon as the hooks of the terminator are drained while the
// application is terminating.
func (t *Terminator) drainedChannel() <-chan struct{} {
	if t.state == nil {
		return nil
	}

	return t.state.drained
}

// Checks whether the terminator is available and fails the application if it is not.
func (p *Plumber) ensureTerminator() bool {
	if !p.Terminator.Enabled {
		p.SendFatal(nil, fmt.Errorf("Plumber does not have the Terminator enabled."))

		return false
	}

	return true
}

/*
Registers a component that is running to the terminator with the hook that should fire whenever the
application is terminating.

The returned function releases the component again as soon as it is done running, therefore only the
components that are still running while the application is terminating have their hooks fired.
*/
func (p *Plumber) registerTerminatorHook(fn terminatorHookFn) func() {
	if !p.Terminator.Enabled {
		return func() {}
	}

	handle := p.Terminator.state.register(fn)

	if handle == 0 {
		return func() {}
	}

	return func() {
		p.Terminator.state.release(handle)
	}
}

/*
Runs the hooks of the components that are registered to the terminator and waits until all of them
are done or until the timeout of the terminator is over.

Every hook runs inside its own routine under the context of the shutdown, so a hook that hangs can
never hold the termination of the application longer than the timeout allows.
*/
func (p *Plumber) drainTerminator(hooks []terminatorHookFn) {
	if !p.Terminator.Enabled {
		return
	}

	log := p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_plumber_terminator,
	})

	for _, hook := range hooks {
		go func() {
			defer p.Terminator.state.done()

			ctx, cancel := p.shutdownContext()
			defer cancel()

			hook(ctx)
		}()
	}

	select {
	case <-p.Terminator.state.drained:
		log.Traceln("Gracefully terminated through terminator.")
	case <-time.After(p.options.timeout):
		log.Warnf("Forcefully terminated since hooks did not finish in time: %d", p.Terminator.state.count())

		p.Terminator.state.forceDrain()
	}
}

// Registers a component to the terminator and hands out the handle it is registered with, which is
// zero whenever the application is already terminating and no new component can be registered.
func (t *terminatorState) register(fn terminatorHookFn) int {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.initiated {
		return 0
	}

	t.handle++

	if fn == nil {
		t.anonymous++
	} else {
		t.hooks[t.handle] = fn
	}

	return t.handle
}

// Releases a component that is registered to the terminator, which can be called more than once for
// the same component and never counts below zero.
func (t *terminatorState) release(handle int) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if handle > 0 {
		if _, ok := t.hooks[handle]; !ok {
			return
		}

		delete(t.hooks, handle)
	} else if t.anonymous > 0 {
		t.anonymous--
	}

	t.checkDrained()
}

// Marks the terminator as initiated and hands over the hooks of the components that are still
// running, which are the responsibility of the drain from this point on.
func (t *terminatorState) initiate() []terminatorHookFn {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.initiated {
		return nil
	}

	t.initiated = true

	hooks := make([]terminatorHookFn, 0, len(t.hooks))

	for handle, fn := range t.hooks {
		hooks = append(hooks, fn)

		delete(t.hooks, handle)
	}

	t.draining = len(hooks)

	t.checkDrained()

	return hooks
}

// Marks a hook that is running while the application is terminating as done.
func (t *terminatorState) done() {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.draining > 0 {
		t.draining--
	}

	t.checkDrained()
}

// Marks the terminator as drained even though the hooks did not finish in time.
func (t *terminatorState) forceDrain() {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.close()
}

func (t *terminatorState) isInitiated() bool {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.initiated
}

// Returns the amount of the components that the terminator is still waiting for.
func (t *terminatorState) count() int {
	t.lock.Lock()
	defer t.lock.Unlock()

	return len(t.hooks) + t.anonymous + t.draining
}

// Unblocks everything that is waiting for the terminator as soon as nothing is registered anymore.
func (t *terminatorState) checkDrained() {
	if !t.initiated || len(t.hooks)+t.anonymous+t.draining > 0 {
		return
	}

	t.close()
}

func (t *terminatorState) close() {
	if t.closed {
		return
	}

	t.closed = true

	close(t.drained)
}

// Validates the current pipe of the task list.
func (p *Plumber) Validate(data any) error {
	if err := defaults.Set(data); err != nil {
		return fmt.Errorf("Can not set defaults: %w", err)
	}

	err := p.Validator.Struct(data)

	if err != nil {
		//nolint:errcheck, errorlint
		for _, err := range err.(validator.ValidationErrors) {
			e := fmt.Sprintf(
				`"%s" field failed validation: %s`,
				err.Namespace(),
				err.Tag(),
			)

			param := err.Param()
			if param != "" {
				e = fmt.Sprintf("%s > %s", e, param)
			}

			p.Log.Errorln(e)
		}

		return fmt.Errorf("Validation failed.")
	}

	return nil
}

/*
Runs a the provided job.

A flow that is started through RunJobs never belongs to another flow, since guessing a parent
chains flows that have nothing to do with each other and lets the one that finishes first cancel
the other one, therefore only the shutdown of the application can cancel it.
*/
func (p *Plumber) RunJobs(job Job) error {
	return p.runJobs(p.context, job)
}

// Runs the provided job as a flow that belongs to the flow of the given context.
func (p *Plumber) RunJobsWith(parent context.Context, job Job) error {
	return p.runJobs(parent, job)
}

/*
Runs the given job as a flow of its own.

Every flow gets a context of its own that is derived from the flow it belongs to and that is
cancelled again as soon as the job returns, therefore a flow that is over can only cancel itself
and never the flow that comes after it or the flow that runs next to it, while the jobs it has
left running in the background are stopped together with it. Cancelling a parent, through the
terminator, a fatal error or a failing job, still cancels every flow that is derived from it.
*/
func (p *Plumber) runJobs(parent context.Context, job Job) error {
	if job == nil {
		return nil
	}

	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	err := job(ctx)

	// Shutting down the application cancels every flow that is running at the moment, which is
	// not a failure of the flow itself but the application ending on purpose, therefore such an
	// error never reaches the caller and never turns into a second fatal error or exit code.
	if causedByCancellation(ctx, err) && errors.Is(context.Cause(p.context), ErrShutdown) {
		return nil
	}

	return err
}

// Cancels the root context of the application with the given reason, so the cancellation reaches
// every flow that is derived from it and every flow can tell that the application is going down.
func (p *Plumber) shutdown(reason string) {
	p.cancel(fmt.Errorf("%s: %w", reason, ErrShutdown))
}

/*
Ends the application with the given exit code.

The application can only go down once, therefore the first caller runs the whole sequence while
every caller that comes after it is ignored: the root context is cancelled with the given reason,
the hooks of the components that are registered to the terminator are drained, the action that is
set for the termination of the application runs and the process finally exits.
*/
func (p *Plumber) exit(reason string, code int) {
	p.exitOnce.Do(func() {
		// The hooks are collected before the flows are cancelled, since a component that is running
		// releases itself as soon as its flow is over and would never have its hook fired otherwise.
		var hooks []terminatorHookFn

		if p.Terminator.Enabled {
			hooks = p.Terminator.state.initiate()
		}

		p.shutdown(reason)

		p.drainTerminator(hooks)

		if p.onTerminateFn != nil {
			p.SendError(nil, p.onTerminateFn())
			p.onTerminateFn = nil
		}

		p.exitFn(code)
	})
}

/*
Creates the context that the hooks which run while the application is terminating are bound to.

The hooks are detached from the cancellation of the application on purpose, since a hook that is
handed the context of a flow that is already cancelled can not even start a command anymore, and
are bound to the timeout of the terminator instead so they can never hold the shutdown forever.
*/
func (p *Plumber) shutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(p.context), p.options.timeout)
}

// Starts the application.
func (p *Plumber) Run() {
	if err := p.loadEnvironment(); err != nil {
		p.SendFatal(nil, err)
	}

	if slices.Contains(os.Args, "MARKDOWN_DOC") ||
		slices.Contains(os.Args, "MARKDOWN_EMBED") {
		p.Cli.SkipFlagParsing = true
	}

	p.Cli.Commands = append(
		p.Cli.Commands,
		&cli.Command{
			Name:            "MARKDOWN_DOC",
			Hidden:          true,
			SkipFlagParsing: true,
			Action: func(_ context.Context, _ *cli.Command) error {
				p.Log.Infoln("Only running the documentation generation without the CLI.")

				return p.generateMarkdownDocumentation()
			},
		},

		&cli.Command{
			Name:            "MARKDOWN_EMBED",
			Hidden:          true,
			SkipFlagParsing: true,
			Action: func(_ context.Context, _ *cli.Command) error {
				p.Log.Infoln("Only running the documentation generation to embed to file without the CLI.")

				return p.embedMarkdownDocumentation()
			},
		},
	)

	if p.options.greeter != nil {
		if err := p.options.greeter(p); err != nil {
			p.SendFatal(nil, err)

			return
		}
	}

	if err := p.Cli.Run(p.context, append(os.Args, strings.Split(os.Getenv("CLI_ARGS"), " ")...)); err != nil {
		p.SendFatal(nil, err)
	}
}

// Prints out DeprecationNotices.
func (p *Plumber) deprecationNoticeHandler() error {
	if len(p.options.deprecationNotices) == 0 {
		return nil
	}

	exit := false
	log := p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_plumber_parser,
	})

	for _, notice := range p.options.deprecationNotices {
		if notice.Level == LOG_LEVEL_DEFAULT {
			notice.Level = LOG_LEVEL_WARN
		}

		if notice.Message == "" && notice.Level <= LOG_LEVEL_ERROR {
			notice.Message = `"%s" is deprecated and is not valid anymore.`
		} else if notice.Message == "" {
			notice.Message = `"%s" is deprecated and will be removed in a later release.`
		}

		for _, environment := range notice.Environment {
			if os.Getenv(environment) != "" {
				log.Logf(notice.Level, notice.Message, fmt.Sprintf("$%s", environment))

				if notice.Level <= LOG_LEVEL_ERROR {
					exit = true
				}
			}
		}

		for _, flag := range notice.Flag {
			if slices.Contains(os.Args, flag) {
				log.Log(notice.Level, notice.Message, flag)

				if notice.Level <= LOG_LEVEL_ERROR {
					exit = true
				}
			}
		}
	}

	if exit {
		return fmt.Errorf("Quitting since deprecation notices can cause unintended behavior.")
	}

	return nil
}

// Appends the default CLI flags to the application.
func (p *Plumber) appendDefaultFlags(flags []cli.Flag) []cli.Flag {
	f := []cli.Flag{}

	f = append(f, CliDefaultFlags...)
	f = append(f, flags...)

	return f
}

// Loads the given environment file to the application.
func (p *Plumber) loadEnvironment() error {
	if v, exists := os.LookupEnv("ENV_FILE"); exists {
		env := strings.Split(v, ",")
		if err := godotenv.Load(env...); err != nil {
			return fmt.Errorf("Can not load environment files: %w", err)
		}

		// no need to long since we do this too early before logger level is properly set
		// p.Log.WithFields(logrus.Fields{
		// 	LOG_FIELD_CONTEXT: p.Cli.Name,
		// 	LOG_FIELD_STATUS:  log_status_plumber_environment,
		// }).
		// 	Tracef("Environment files are loaded: %v", env)
	}

	return nil
}

// Before function for the CLI that gets executed setup the action.
func (p *Plumber) setup(before cli.BeforeFunc) cli.BeforeFunc {
	return func(ctx context.Context, command *cli.Command) (context.Context, error) {
		if command.Bool("debug") || p.Log.Level == LOG_LEVEL_DEBUG || p.Log.Level == LOG_LEVEL_TRACE {
			p.Environment.Debug = true
		}

		if err := p.setupLogger(command); err != nil {
			return nil, err
		}

		log := p.Log.WithFields(logrus.Fields{
			LOG_FIELD_CONTEXT: command.Name,
			LOG_FIELD_STATUS:  log_status_plumber_setup,
		})

		if command.Bool("ci") {
			log.Traceln("Running inside CI.")

			p.Environment.CI = true
		}

		if before != nil {
			if ctx, err := before(ctx, command); err != nil {
				return ctx, err
			}
		}

		if err := p.deprecationNoticeHandler(); err != nil {
			return ctx, err
		}

		return ctx, nil
	}
}

// Sets up logger for the application.
//
//nolint:unparam
func (p *Plumber) setupLogger(command *cli.Command) error {
	level, err := logrus.ParseLevel(command.String("log-level"))

	if err != nil {
		level = logrus.InfoLevel
	}

	if command.Bool("debug") {
		level = logrus.DebugLevel
	}

	p.Log.SetLevel(level)

	if p.Environment.Debug {
		p.Log.SetReportCaller(true)
	}

	p.Log.ExitFunc = p.Terminate

	log := p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_plumber_setup,
	})

	log.Tracef("Logger has been setup with level: %s", p.Log.GetLevel().String())

	if p.Environment.Debug {
		log.Traceln("Running in debug mode.")
	}

	return nil
}

// Registers the os.Signal listener for the application.
func (p *Plumber) registerInterruptHandler() {
	interrupt := make(chan os.Signal, 1)

	signal.Notify(interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	go func() {
		sig := <-interrupt

		p.Log.WithFields(logrus.Fields{
			LOG_FIELD_CONTEXT: p.Cli.Name,
			LOG_FIELD_STATUS:  log_status_plumber_terminator,
		}).Errorf(
			"Terminating the application with signal: %s",
			sig,
		)

		p.SendTerminate(sig, 127)
	}()

	p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_plumber_setup,
	}).Traceln("Registered handlers.")
}

// Greet the user with the application name and version.
func greeter(p *Plumber) error {
	var version = p.Cli.Version

	// if version == "latest" || version == "" {
	// 	version = fmt.Sprintf("BUILD.%s", p.Cli.Compiled.UTC().Format("20060102Z1504"))
	// }

	name := fmt.Sprintf("%s - %s", p.Cli.Name, version)
	//revive:disable:unhandled-error
	fmt.Println(name)
	fmt.Println(strings.Repeat("-", len(name)))
	//revive:enable:unhandled-error

	return nil
}
