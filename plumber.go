package plumber

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gitlab.kilic.dev/libraries/go-broadcaster"
	"gitlab.kilic.dev/libraries/go-utils/logger"
	"golang.org/x/exp/slices"
)

type Plumber struct {
	Cli         *cli.App
	Log         *logrus.Logger
	Environment AppEnvironment
	Channel     AppChannel
	Terminator

	PlumberOnTerminateFn
	secrets []string

	DocsFile                        string
	DocsExcludeFlags                bool
	DocsExcludeEnvironmentVariables bool
	DocsExcludeHelpCommand          bool

	DeprecationNotices []DeprecationNotice
}

type AppEnvironment struct {
	Debug bool
	CI    bool
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
	Err chan error
	// to communicate the errors while not blocking
	CustomErr chan ErrorWithLogger
	// Fatal errors
	Fatal chan error
	// to communicate the errors while not blocking
	CustomFatal chan ErrorWithLogger
	// terminate channel
	Interrupt chan os.Signal
	// exit channel
	Exit *broadcaster.Broadcaster[int]
}

type DeprecationNotice struct {
	Message     string
	Environment string
	Flag        string
	Level       LogLevel
}

type (
	PlumberOnTerminateFn func() error
	ErrorWithLogger      struct {
		Log *logrus.Entry
		Err error
	}
)

// Cli.New Creates a new plumber for pipes.
func (p *Plumber) New(
	fn func(a *Plumber) *cli.App,
) *Plumber {
	p.Cli = fn(p)

	// have to this here too, to catch cli errors, but it is singleton so not much harm
	p.Log = logger.InitiateLogger(logrus.InfoLevel)
	p.setupLogger(logrus.InfoLevel)

	p.Cli.Before = p.setup(p.Cli.Before)

	if p.Cli.Action == nil {
		p.Cli.Action = p.defaultAction()

		p.Log.Traceln("There was no action set so using the default one.")
	}

	p.Cli.Flags = p.appendDefaultFlags(p.Cli.Flags)

	if p.DocsFile == "" {
		p.DocsFile = "README.md"
	}

	// if len(p.Cli.Commands) > 0 {
	// 	for i, v := range p.Cli.Commands {
	// 		p.Cli.Commands[i].Flags = p.appendDefaultFlags(v.Flags)
	// 	}
	// }

	p.Environment = AppEnvironment{}

	// create error channels
	p.Channel = AppChannel{
		Err:         make(chan error),
		CustomErr:   make(chan ErrorWithLogger),
		Fatal:       make(chan error),
		CustomFatal: make(chan ErrorWithLogger),
		Interrupt:   make(chan os.Signal),
		Exit:        broadcaster.NewBroadcaster[int](0),
	}

	p.Terminator = Terminator{
		Enabled: false,
	}

	return p
}

// Cli.Run Starts the application.
func (p *Plumber) Run() {
	p.Cli.Setup()

	p.greet()

	go p.registerHandlers()

	if slices.Contains(os.Args, "MARKDOWN_DOC") {
		p.setupLogger(LOG_LEVEL_TRACE)

		p.Log.Traceln("Only running the documentation generation without the CLI.")

		if err := p.generateMarkdownDocumentation(); err != nil {
			p.Log.Fatalln(err)
		}

		os.Exit(0)
	}

	p.deprecationNoticeHandler()

	if err := p.Cli.Run(os.Args); err != nil {
		p.SendFatal(err)

		ch := make(chan int, 1)
		p.Channel.Exit.Register(ch)
		for {
			<-ch
		}
	}
}

// Cli.AppendFlags Appends flags together.
func (p *Plumber) AppendFlags(flags ...[]cli.Flag) []cli.Flag {
	f := []cli.Flag{}

	for _, v := range flags {
		f = append(f, v...)
	}

	return f
}

// Cli.SetOnTerminate Sets the action that would be executed on terminate.
func (p *Plumber) SetOnTerminate(fn PlumberOnTerminateFn) *Plumber {
	p.PlumberOnTerminateFn = fn

	return p
}

func (p *Plumber) EnableTerminator() *Plumber {
	p.Terminator = Terminator{
		Enabled:         true,
		Lock:            &sync.RWMutex{},
		ShouldTerminate: broadcaster.NewBroadcaster[os.Signal](1),
		Terminated:      broadcaster.NewBroadcaster[bool](1),
	}

	p.Log.Traceln("Terminator enabled.")

	return p
}

func (p *Plumber) AppendSecrets(secrets ...string) *Plumber {
	p.secrets = append(p.secrets, secrets...)

	return p
}

func (p *Plumber) SendError(err error) *Plumber {
	p.Channel.Err <- err

	return p
}

func (p *Plumber) SendCustomError(log *logrus.Entry, err error) *Plumber {
	p.Channel.CustomErr <- ErrorWithLogger{
		Err: err,
		Log: log,
	}

	return p
}

func (p *Plumber) SendFatal(err error) *Plumber {
	p.Channel.Fatal <- err

	return p
}

func (p *Plumber) SendCustomFatal(log *logrus.Entry, err error) *Plumber {
	p.Channel.CustomFatal <- ErrorWithLogger{
		Err: err,
		Log: log,
	}

	return p
}

func (p *Plumber) SendExit(code int) *Plumber {
	p.Log.Tracef("Exit: %d", code)

	p.Channel.Exit.Submit(code)

	return p
}

func (p *Plumber) RegisterTerminated() *Plumber {
	if !p.Terminator.Enabled {
		p.SendFatal(fmt.Errorf("Plumber does not have the Terminator enabled."))

		return p
	}

	if p.Terminator.registered > 0 {
		p.Terminator.Lock.Lock()
		p.Terminator.terminated++
		p.Terminator.Lock.Unlock()
		p.Log.Tracef("Received new terminated signal: %d out of %d", p.Terminator.terminated, p.Terminator.registered)

		if p.Terminator.terminated < p.Terminator.registered {
			return p
		}

		p.Log.Tracef("Enough votes received for termination.")
	}

	p.Terminator.Terminated.Submit(true)

	return p
}

func (p *Plumber) RegisterTerminator() *Plumber {
	if !p.Terminator.Enabled {
		p.SendFatal(fmt.Errorf("Plumber does not have the Terminator enabled."))

		return p
	}

	p.Terminator.Lock.Lock()
	p.Terminator.registered++
	p.Terminator.Lock.Unlock()

	return p
}

// Cli.greet Greet the user with the application name and version.
func (p *Plumber) greet() {
	var version = p.Cli.Version

	if version == "latest" || version == "" {
		version = fmt.Sprintf("BUILD.%s", p.Cli.Compiled.UTC().Format("20060102Z1504"))
	}

	name := fmt.Sprintf("%s - %s", p.Cli.Name, version)
	//revive:disable:unhandled-error
	fmt.Println(name)
	fmt.Println(strings.Repeat("-", len(name)))
	//revive:enable:unhandled-error
}

func (p *Plumber) deprecationNoticeHandler() {
	if len(p.DeprecationNotices) == 0 {
		return
	}

	for _, notice := range p.DeprecationNotices {
		if notice.Level == LOG_LEVEL_DEFAULT {
			notice.Level = LOG_LEVEL_WARN
		}

		if notice.Message == "" {
			notice.Message = `"%s" (%s) is deprecated and will be removed in a latter release.`
		}

		if notice.Environment != "" && os.Getenv(notice.Environment) != "" {
			p.Log.Log(notice.Level, notice.Message, notice.Environment, "environment variable")
		}

		if notice.Flag != "" && slices.Contains(os.Args, notice.Flag) {
			p.Log.Log(notice.Level, notice.Message, notice.Flag, "flag")
		}
	}
}

func (p *Plumber) appendDefaultFlags(flags []cli.Flag) []cli.Flag {
	f := []cli.Flag{}

	f = append(f, CliDefaultFlags...)
	f = append(f, flags...)

	return f
}

// Cli.loadEnvironment Loads the given environment file to the application.
func (p *Plumber) loadEnvironment() error {
	if env := os.Getenv("ENV_FILE"); env != "" {
		if err := godotenv.Load(env); err != nil {
			return err
		}

		p.Log.Traceln("Environment file loaded.")
	}

	return nil
}

// Cli.setup Before function for the CLI that gets executed setup the action.
func (p *Plumber) setup(before cli.BeforeFunc) cli.BeforeFunc {
	return func(ctx *cli.Context) error {
		if err := p.loadEnvironment(); err != nil {
			return err
		}

		level, err := logrus.ParseLevel(ctx.String("log-level"))

		if err != nil {
			level = logrus.InfoLevel
		}

		if ctx.Bool("debug") {
			level = logrus.DebugLevel

			p.Environment.Debug = true
		}

		if ctx.Bool("ci") {
			p.Environment.CI = true
		}

		p.setupLogger(level)

		if before != nil {
			if err := before(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

func (p *Plumber) setupLogger(level LogLevel) {
	p.Log.Level = level

	p.Log.SetFormatter(
		&logger.Formatter{
			FieldsOrder:      []string{LOG_FIELD_CONTEXT, LOG_FIELD_STATUS},
			TimestampFormat:  "",
			HideKeys:         true,
			NoColors:         false,
			NoFieldsColors:   false,
			NoFieldsSpace:    false,
			ShowFullLevel:    false,
			NoUppercaseLevel: false,
			TrimMessages:     true,
			CallerFirst:      false,
			Secrets:          &p.secrets,
		},
	)

	p.Log.ExitFunc = p.Terminate

	p.Log.Traceln("Logger setup.")
}

func (p *Plumber) defaultAction() cli.ActionFunc {
	return func(ctx *cli.Context) error {
		if err := cli.ShowAppHelp(ctx); err != nil {
			return err
		}

		return fmt.Errorf("Application needs a subcommand to run.")
	}
}

// App.registerInterruptHandler Registers the os.Signal listener for the application.
func (p *Plumber) registerInterruptHandler() {
	signal.Notify(p.Channel.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	interrupt := <-p.Channel.Interrupt
	p.Log.Errorf(
		"Terminating the application with signal: %s",
		interrupt,
	)

	p.SendTerminate(interrupt, 127)
}

func (p *Plumber) SendTerminate(sig os.Signal, code int) {
	if p.Terminator.Enabled {
		if p.Terminator.initiated {
			p.Log.Tracef("Termination process already started, ignoring: %s", sig)

			return
		}

		p.Log.Tracef("Sending should terminate through terminator: %s", sig)

		p.Terminator.ShouldTerminate.Submit(sig)

		p.Terminator.Lock.Lock()
		p.Terminator.initiated = true
		p.Terminator.Lock.Unlock()
	}

	p.Terminate(code)
}

func (p *Plumber) registerHandlers() {
	go p.registerErrorHandler()
	go p.registerFatalErrorHandler()
	go p.registerInterruptHandler()
	go p.registerExitHandler()

	p.Log.Traceln("Registered handlers.")
}

// App.registerErrorHandler Registers the error handlers for the runtime errors, this will not terminate application.
func (p *Plumber) registerErrorHandler() {
	for {
		select {
		case err := <-p.Channel.Err:
			if err == nil {
				continue
			}

			if p.Log != nil {
				p.Log.Errorln(err)
			} else {
				panic(err.Error())
			}
		case err := <-p.Channel.CustomErr:
			if err.Err == nil {
				continue
			}

			err.Log.Errorln(err.Err)
		}
	}
}

// App.registerFatalErrorHandler Registers the error handler for fatal errors, this will terminate the application.
func (p *Plumber) registerFatalErrorHandler() {
	for {
		select {
		case err := <-p.Channel.Fatal:
			if err == nil {
				continue
			}

			if p.Log != nil {
				p.Log.Fatalln(err)
			} else {
				panic(err.Error())
			}
		case err := <-p.Channel.CustomFatal:
			if err.Err == nil {
				continue
			}

			err.Log.Fatalln(err.Err)
		}
	}
}

func (p *Plumber) registerExitHandler() {
	ch := make(chan int, 1)

	p.Channel.Exit.Register(ch)

	code := <-ch

	defer p.Channel.Exit.Unregister(ch)
	defer p.Channel.Exit.Close()

	if p.Terminator.Enabled {
		p.Terminator.ShouldTerminate.Close()
		p.Terminator.Terminated.Close()
	}

	close(p.Channel.Interrupt)
	close(p.Channel.Err)
	close(p.Channel.CustomErr)
	close(p.Channel.Fatal)
	close(p.Channel.CustomFatal)

	os.Exit(code)
}

// App.Terminate Terminates the application.
func (p *Plumber) Terminate(code int) {
	if p.Terminator.Enabled {
		if p.Terminator.registered > 0 {
			if !p.Terminator.initiated {
				p.SendTerminate(syscall.SIGSTOP, 1)

				return
			}

			p.Log.Traceln("Waiting for result through terminator...")

			ch := make(chan bool, 1)

			p.Terminator.Terminated.Register(ch)
			defer p.Terminator.Terminated.Unregister(ch)

			<-ch

			p.Log.Traceln("Gracefully terminated through terminator.")
		}
	}

	if p.PlumberOnTerminateFn != nil {
		p.SendError(p.PlumberOnTerminateFn())
	}

	p.SendExit(code)
}
