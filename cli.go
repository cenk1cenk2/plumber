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
)

type App struct {
	Cli         *cli.App
	Log         *logrus.Logger
	Environment AppEnvironment
	Channel     AppChannel
}

type AppEnvironment struct {
	Debug bool
	CI    bool
}

type AppChannel struct {
	// to communicate the errors while not blocking
	Err chan error
	// Fatal errors
	Fatal chan error
	// terminate channel
	Interrupt chan os.Signal
}

// Cli.New Creates a new plumber for pipes.
func (a *App) New(
	fn func(a *App) *cli.App,
) *App {
	a.Cli = fn(a)

	a.Cli.Before = a.setup(a.Cli.Before)

	if a.Cli.Action == nil {
		a.Cli.Action = a.defaultAction()
	}

	a.Cli.Flags = a.appendDefaultFlags(a.Cli.Flags)

	if len(a.Cli.Commands) > 0 {
		for i, v := range a.Cli.Commands {
			a.Cli.Commands[i].Flags = a.appendDefaultFlags(v.Flags)
		}
	}

	a.Environment = AppEnvironment{}

	// create error channels
	a.Channel.Err = make(chan error)
	a.Channel.Fatal = make(chan error, 1)
	a.Channel.Interrupt = make(chan os.Signal)

	return a
}

// Cli.Run Starts the application.
func (a *App) Run() *App {
	a.greet()

	go func() {
		go a.registerErrorHandler()
		go a.registerFatalErrorHandler()
		go a.registerInterruptHandler()
	}()

	if err := a.Cli.Run(os.Args); err != nil {
		a.Channel.Fatal <- err
	}

	return a
}

func (a *App) AppendFlags(flags ...[]cli.Flag) []cli.Flag {
	f := []cli.Flag{}

	for _, v := range flags {
		f = append(f, v...)
	}

	return f
}

// Cli.greet Greet the user with the application name and version.
func (a *App) greet() {
	name := fmt.Sprintf("%s - %s", a.Cli.Name, a.Cli.Version)
	fmt.Println(name)
	fmt.Println(strings.Repeat("-", len(name)))
}

func (a *App) appendDefaultFlags(flags []cli.Flag) []cli.Flag {
	f := []cli.Flag{}

	f = append(f, CliDefaultFlags...)
	f = append(f, flags...)

	return f
}

// Cli.loadEnvironment Loads the given environment file to the application.
func (a *App) loadEnvironment() error {
	if env := os.Getenv("ENV_FILE"); env != "" {
		if err := godotenv.Load(env); err != nil {
			return err
		}
	}

	return nil
}

// Cli.setup Before function for the CLI that gets executed setup the action.
func (a *App) setup(before cli.BeforeFunc) cli.BeforeFunc {
	return func(ctx *cli.Context) error {
		err := a.loadEnvironment()

		if err != nil {
			return err
		}

		level, err := logrus.ParseLevel(ctx.String("log_level"))

		if err != nil {
			level = logrus.InfoLevel
		}

		if ctx.Bool("debug") {
			level = logrus.DebugLevel

			a.Environment.Debug = true
		}

		if ctx.Bool("ci") {
			a.Environment.CI = true
		}

		a.Log = logger.InitiateLogger(level)

		if before != nil {
			if err := before(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

func (a *App) defaultAction() cli.ActionFunc {
	return func(ctx *cli.Context) error {
		if err := cli.ShowAppHelp(ctx); err != nil {
			return err
		}

		return fmt.Errorf("Application needs a subcommand to run.")
	}
}

// App.registerInterruptHandler Registers the os.Signal listener for the application.
func (a *App) registerInterruptHandler() {
	signal.Notify(a.Channel.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	interrupt := <-a.Channel.Interrupt
	a.Log.Errorf(
		"Terminating the application with operating system signal: %s",
		interrupt,
	)

	a.Terminate(1)
}

// App.registerErrorHandler Registers the error handlers for the runtime errors, this will not terminate application.
func (a *App) registerErrorHandler() {
	for {
		err := <-a.Channel.Err
		if err == nil {
			return
		}

		a.Log.Errorln(err)
	}
}

// App.registerFatalErrorHandler Registers the error handler for fatal errors, this will terminate the application.
func (a *App) registerFatalErrorHandler() {
	for {
		err := <-a.Channel.Fatal
		if err == nil {
			return
		}

		a.Terminate(127)
	}
}

// App.Terminate Terminates the application.
func (a *App) Terminate(code int) {
	close(a.Channel.Err)
	close(a.Channel.Fatal)
	close(a.Channel.Interrupt)

	os.Exit(code)
}
