package plumber

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gitlab.kilic.dev/libraries/go-utils/logger"
	"golang.org/x/exp/slices"
)

type Plumber struct {
	Cli         *cli.App
	Log         *logrus.Logger
	Environment AppEnvironment
	Channel     AppChannel
	Terminator

	onTerminateFn

	DocsFile                        string
	DocsExcludeFlags                bool
	DocsExcludeEnvironmentVariables bool
	DocsExcludeHelpCommand          bool
}

type AppEnvironment struct {
	Debug bool
	CI    bool
}

type Terminator struct {
	Enabled         bool
	ShouldTerminate chan os.Signal
	Terminated      chan bool
	Count           uint
	terminated      uint
	registered      uint
}

type AppChannel struct {
	// to communicate the errors while not blocking
	Err chan error
	// to communicate the errors while not blocking
	CustomErr chan ErrorChannelWithLogger
	// Fatal errors
	Fatal chan error
	// to communicate the errors while not blocking
	CustomFatal chan ErrorChannelWithLogger
	// terminate channel
	Interrupt chan os.Signal
	// exit channel
	Exit chan int
}

type (
	onTerminateFn          func() error
	ErrorChannelWithLogger struct {
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
		CustomErr:   make(chan ErrorChannelWithLogger),
		Fatal:       make(chan error),
		CustomFatal: make(chan ErrorChannelWithLogger),
		Interrupt:   make(chan os.Signal),
		Exit:        make(chan int, 1),
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

	if err := p.Cli.Run(os.Args); err != nil {
		p.SendFatal(err)

		for {
			// to be sure that os.exit is completed
			<-p.Channel.Exit
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
func (p *Plumber) SetOnTerminate(fn onTerminateFn) *Plumber {
	p.onTerminateFn = fn

	return p
}

func (p *Plumber) EnableTerminator() *Plumber {
	p.Terminator = Terminator{
		Enabled:         true,
		ShouldTerminate: make(chan os.Signal),
		Terminated:      make(chan bool),
	}

	p.Log.Traceln("Terminator enabled.")

	return p
}

func (p *Plumber) SetTerminatorCount(count uint) *Plumber {
	p.Terminator.Count = count

	return p
}

func (p *Plumber) SendError(err error) *Plumber {
	p.Channel.Err <- err

	return p
}

func (p *Plumber) SendCustomError(log *logrus.Entry, err error) *Plumber {
	p.Channel.CustomErr <- ErrorChannelWithLogger{
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
	p.Channel.CustomFatal <- ErrorChannelWithLogger{
		Err: err,
		Log: log,
	}

	return p
}

func (p *Plumber) SendExit(code int) *Plumber {
	p.Channel.Exit <- code

	return p
}

func (p *Plumber) SendTerminated() *Plumber {
	if !p.Terminator.Enabled {
		p.SendFatal(fmt.Errorf("Plumber does not have the Terminator enabled."))

		return p
	}

	if p.Terminator.registered > 1 {
		if p.Terminator.terminated < p.Terminator.registered {
			p.Terminator.terminated++
			p.Log.Tracef("Received new terminated signal: %d out of %d expected %d", p.Terminator.terminated, p.Terminator.registered, p.Terminator.Count)

			return p
		}

		p.Log.Tracef("Enough votes for terminating!")
	}

	p.Terminator.Terminated <- true

	return p
}

func (p *Plumber) RegisterTerminator() *Plumber {
	if !p.Terminator.Enabled {
		p.SendFatal(fmt.Errorf("Plumber does not have the Terminator enabled."))

		return p
	}

	p.Terminator.registered++

	if p.Terminator.registered == p.Terminator.Count {
		p.Log.Tracef("Registered terminators reached the expected count: %d", p.Terminator.registered)
	} else if p.Terminator.registered > p.Terminator.Count {
		p.Log.Tracef("Registered terminators exceeded the expected count, this should be programmatic error!: %d", p.Terminator.registered)
	}

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

	if p.Terminator.Enabled {
		p.Log.Traceln("Sending operating system signal through terminator.")
		p.Terminator.ShouldTerminate <- interrupt
	}

	p.Terminate(1)
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
				return
			}

			if p.Log != nil {
				p.Log.Errorln(err)
			} else {
				panic(err.Error())
			}
		case err := <-p.Channel.CustomErr:
			if err.Err == nil {
				return
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
				return
			}

			if p.Log != nil {
				p.Log.Fatalln(err)
			} else {
				panic(err.Error())
			}
		case err := <-p.Channel.CustomFatal:
			if err.Err == nil {
				return
			}

			err.Log.Fatalln(err.Err)
		}
	}
}

func (p *Plumber) registerExitHandler() {
	os.Exit(<-p.Channel.Exit)
}

// App.Terminate Terminates the application.
func (p *Plumber) Terminate(code int) {
	if p.onTerminateFn != nil {
		p.SendError(p.onTerminateFn())
	}

	if p.Terminator.Enabled {
		p.Log.Traceln("Sending should terminate through terminator.")
		p.Terminator.ShouldTerminate <- syscall.SIGSTOP

		p.Log.Traceln("Waiting for result through terminator...")

		if p.Terminator.registered > 0 {
			<-p.Terminator.Terminated
		} else {
			p.Log.Tracef("Nothing registered in the terminator while expecting %d.", p.Terminator.Count)
		}

		p.Log.Traceln("Terminated through terminator.")

		close(p.Terminator.ShouldTerminate)
		close(p.Terminator.Terminated)
	}

	close(p.Channel.Err)
	close(p.Channel.CustomErr)
	close(p.Channel.Fatal)
	close(p.Channel.CustomFatal)
	close(p.Channel.Interrupt)

	p.SendExit(code)
}
