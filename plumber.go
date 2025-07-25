package plumber

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creasty/defaults"
	validator "github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	"github.com/workanator/go-floc/v3"
	"gitlab.kilic.dev/libraries/go-broadcaster"
	"gitlab.kilic.dev/libraries/go-utils/v2/logger"
	"golang.org/x/exp/slices"
)

type Plumber struct {
	Cli         *cli.Command
	Log         *logrus.Logger
	Environment AppEnvironment
	Channel     AppChannel
	Terminator
	Validator *validator.Validate

	context     context.Context
	cancel      context.CancelFunc
	flocControl floc.Control
	flocContext floc.Context

	secrets       []string
	onTerminateFn PlumberOnTerminateFn
	options       PlumberOptions
}

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

type PlumberError struct {
	Log *logrus.Entry
	Err error
}

type Terminator struct {
	Enabled         bool
	ShouldTerminate *broadcaster.Broadcaster[os.Signal]
	Terminated      *broadcaster.Broadcaster[bool]
	Lock            *sync.RWMutex
	terminated      uint
	registered      uint
	initiated       bool
}

type AppChannel struct {
	// to communicate the errors while not blocking
	Err chan PlumberError
	// to communicate the errors while not blocking
	Fatal chan PlumberError
	// terminate channel
	Interrupt chan os.Signal
	// exit channel
	Exit *broadcaster.Broadcaster[int]
}

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

	p.context, p.cancel = context.WithCancel(context.Background())

	p.flocContext = floc.NewContext()
	p.flocControl = floc.NewControl(p.flocContext)

	p.Cli = fn(p)

	// create error channels
	p.Channel = AppChannel{
		Err:       make(chan PlumberError),
		Fatal:     make(chan PlumberError),
		Interrupt: make(chan os.Signal),
		Exit:      broadcaster.NewBroadcaster[int](1),
	}

	p.Terminator = Terminator{
		Enabled: false,
	}

	p.options = PlumberOptions{
		delimiter: ":",
		timeout:   time.Second * 5,
		greeter:   greeter,
	}

	p.Validator = validator.New()

	p.Cli.Before = p.setup(p.Cli.Before)

	p.Cli.Flags = p.appendDefaultFlags(p.Cli.Flags)

	p.Environment = AppEnvironment{}

	// presetup logger to not have it nil in edge cases
	p.Log = logger.InitiateLogger(logrus.InfoLevel)
	// TODO: secrets in the formatter is a pointer and it is empty so it has to be fixed on the library
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
		CallerFirst:      false,
		Secrets:          &p.secrets,
	}
	p.SetFormatter(formatter)

	p.registerHandlers()

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

/*
Enables terminator globally for the current application.

If terminator functions are going to be used inside task lists, tasks and commands, terminator should be globally enabled.
The terminate information will be propagated through the channels to the subcomponents.
*/
func (p *Plumber) EnableTerminator() *Plumber {
	p.Terminator = Terminator{
		Enabled:         true,
		Lock:            &sync.RWMutex{},
		ShouldTerminate: broadcaster.NewBroadcaster[os.Signal](1),
		Terminated:      broadcaster.NewBroadcaster[bool](1),
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

// Sends an error with its custom instance of logger through the channel.
func (p *Plumber) SendError(log *logrus.Entry, err error) *Plumber {
	e := PlumberError{
		Err: err,
		Log: log,
	}

	if e.Log == nil {
		e.Log = p.Log.WithFields(logrus.Fields{})
	}

	p.Channel.Err <- e

	return p
}

// Sends an fatal error with its custom instance of logger through the channel.
func (p *Plumber) SendFatal(log *logrus.Entry, err error) *Plumber {
	p.flocControl.Cancel(err)

	e := PlumberError{
		Err: err,
		Log: log,
	}

	if e.Log == nil {
		e.Log = p.Log.WithFields(logrus.Fields{})
	}

	p.Channel.Fatal <- e

	return p
}

// Sends exit code to terminate the application.
func (p *Plumber) SendExit(code int) *Plumber {
	p.flocControl.Cancel(fmt.Sprintf("Will exit with code: %d", code))

	p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_exit,
	}).Traceln(code)

	p.Channel.Exit.Submit(code)

	p.cancel()

	return p
}

// Sends a terminate request to the application via interruption signal.
func (p *Plumber) SendTerminate(sig os.Signal, code int) {
	if p.Terminator.Enabled {
		log := p.Log.WithFields(logrus.Fields{
			LOG_FIELD_CONTEXT: p.Cli.Name,
			LOG_FIELD_STATUS:  log_status_plumber_terminator,
		})

		if p.Terminator.initiated {
			log.Tracef("Termination process already started, ignoring: %s", sig)

			return
		}

		log.Tracef("Sending should terminate through terminator: %s", sig)

		p.Terminator.ShouldTerminate.Submit(sig)

		p.Terminator.Lock.Lock()
		p.Terminator.initiated = true
		p.Terminator.Lock.Unlock()
	}

	p.Terminate(code)
}

/*
Sends a terminate request through the application.

This will gracefully try to stop the application components that are registered and listening for the terminator.
*/
func (p *Plumber) Terminate(code int) {
	//nolint:nestif
	if p.Terminator.Enabled {
		if p.Terminator.registered > 0 {
			log := p.Log.WithFields(logrus.Fields{
				LOG_FIELD_CONTEXT: p.Cli.Name,
				LOG_FIELD_STATUS:  log_status_plumber_terminator,
			})

			if !p.Terminator.initiated {
				p.SendTerminate(syscall.SIGSTOP, 1)

				return
			}

			log.Tracef("Waiting for result through terminator: %d", p.Terminator.registered)

			ch := make(chan bool, 1)

			p.Terminator.Terminated.Register(ch)
			defer p.Terminator.Terminated.Unregister(ch)

			go func() {
				time.Sleep(p.options.timeout)

				log.Warnf("Forcefully terminated since hooks did not finish in time: %d of %d", p.Terminator.terminated, p.Terminator.registered)

				if p.onTerminateFn != nil {
					p.SendError(nil, p.onTerminateFn())
					p.onTerminateFn = nil
				}

				p.SendExit(code)
			}()

			<-ch

			log.Traceln("Gracefully terminated through terminator.")
		}
	}

	if p.onTerminateFn != nil {
		p.SendError(nil, p.onTerminateFn())
		p.onTerminateFn = nil
	}

	p.SendExit(code)
}

// Registers a new component that should be handled by the terminator.
func (p *Plumber) RegisterTerminator() *Plumber {
	if !p.Terminator.Enabled {
		p.SendFatal(nil, fmt.Errorf("Plumber does not have the Terminator enabled."))

		return p
	}

	p.Terminator.Lock.Lock()
	p.Terminator.registered++
	p.Terminator.Lock.Unlock()

	return p
}

func (p *Plumber) DeregisterTerminator() *Plumber {
	if !p.Terminator.Enabled {
		p.SendFatal(nil, fmt.Errorf("Plumber does not have the Terminator enabled."))

		return p
	}

	p.Terminator.Lock.Lock()
	p.Terminator.registered--
	p.Terminator.Lock.Unlock()

	return p
}

// Register a component as successfully terminated.
func (p *Plumber) RegisterTerminated() *Plumber {
	if !p.Terminator.Enabled {
		p.SendFatal(nil, fmt.Errorf("Plumber does not have the Terminator enabled."))

		return p
	}

	if p.Terminator.registered > 0 {
		log := p.Log.WithFields(logrus.Fields{
			LOG_FIELD_CONTEXT: p.Cli.Name,
			LOG_FIELD_STATUS:  log_status_plumber_terminator,
		})

		p.Terminator.Lock.Lock()
		p.Terminator.terminated++
		p.Terminator.Lock.Unlock()
		log.Tracef("Received new terminated signal: %d out of %d", p.Terminator.terminated, p.Terminator.registered)

		if p.Terminator.terminated < p.Terminator.registered {
			return p
		}

		log.Tracef("Enough votes received for termination.")
	}

	p.Terminator.Terminated.Submit(true)

	return p
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

// Runs a the provided job.
func (p *Plumber) RunJobs(job Job) error {
	if job == nil {
		return nil
	}

	result, data, err := floc.RunWith(p.flocContext, p.flocControl, job)

	if err != nil {
		return err
	}

	return p.handleFloc(result, data)
}

// Handles output coming from floc.
func (p *Plumber) handleFloc(_ floc.Result, _ interface{}) error {
	return nil
}

// Starts the application.
func (p *Plumber) Run() {
	ch := make(chan int, 1)
	p.Channel.Exit.Register(ch)

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

		for {
			<-ch
		}
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
func (p *Plumber) loadEnvironment(command *cli.Command) error {
	if env := command.StringSlice("env-file"); len(env) != 0 {
		if err := godotenv.Load(env...); err != nil {
			return err
		}

		p.Log.WithFields(logrus.Fields{
			LOG_FIELD_CONTEXT: p.Cli.Name,
			LOG_FIELD_STATUS:  log_status_plumber_environment,
		}).
			Tracef("Environment files are loaded: %v", env)
	}

	return nil
}

// Before function for the CLI that gets executed setup the action.
func (p *Plumber) setup(before cli.BeforeFunc) cli.BeforeFunc {
	return func(ctx context.Context, command *cli.Command) (context.Context, error) {
		if err := p.setupLogger(command); err != nil {
			return nil, err
		}

		if err := p.loadEnvironment(command); err != nil {
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

		if command.Bool("debug") || p.Log.Level == LOG_LEVEL_DEBUG || p.Log.Level == LOG_LEVEL_TRACE {
			p.Environment.Debug = true
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

	p.Log = logger.InitiateLogger(level)
	p.Log.Level = level

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
		CallerFirst:      false,
		Secrets:          &p.secrets,
	}

	if p.Environment.Debug {
		formatter.CallerFirst = true
	}

	p.SetFormatter(formatter)

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
func (p *Plumber) registerInterruptHandler(registered chan string) {
	registered <- "interrupt"

	signal.Notify(p.Channel.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	interrupt := <-p.Channel.Interrupt
	p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_plumber_terminator,
	}).Errorf(
		"Terminating the application with signal: %s",
		interrupt,
	)

	p.SendTerminate(interrupt, 127)
}

//nolint:unparam
func (p *Plumber) registerHandlers() {
	registered := make(chan string, 3)
	count := 0

	go p.registerErrorHandler(registered)
	go p.registerInterruptHandler(registered)
	go p.registerExitHandler(registered)

	for {
		<-registered
		count++

		if count >= 3 {
			break
		}
	}

	p.Log.WithFields(logrus.Fields{
		LOG_FIELD_CONTEXT: p.Cli.Name,
		LOG_FIELD_STATUS:  log_status_plumber_setup,
	}).Traceln("Registered handlers.")

	close(registered)
}

// Registers the error handlers for the runtime errors, this will not terminate application.
func (p *Plumber) registerErrorHandler(registered chan string) {
	registered <- "error"

	for {
		select {
		case err := <-p.Channel.Err:
			if err.Err == nil {
				continue
			}

			err.Log.Errorln(err.Err)
		case err := <-p.Channel.Fatal:
			if err.Err == nil {
				continue
			}

			err.Log.Fatalln(err.Err)
		}
	}
}

// Registers the exit handler that will stop the application with a exit code.
func (p *Plumber) registerExitHandler(registered chan string) {
	registered <- "exit"

	ch := make(chan int, 1)

	p.Channel.Exit.Register(ch)

	code := <-ch

	defer p.Channel.Exit.Unregister(ch)
	defer p.Channel.Exit.Close()

	if p.Terminator.Enabled {
		//nolint:errcheck
		p.Terminator.ShouldTerminate.Close()
		//nolint:errcheck
		p.Terminator.Terminated.Close()
	}

	close(p.Channel.Interrupt)
	close(p.Channel.Err)
	close(p.Channel.Fatal)

	os.Exit(code)
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
