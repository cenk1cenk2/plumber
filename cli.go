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
	Control     AppControl
}

type AppEnvironment struct {
	Debug bool
	CI    bool
}

type AppControl struct {
	// to communicate the errors while not blocking
	Err chan error
	// Fatal errors
	Fatal chan error
	// terminate channel
	Interrupt chan os.Signal
}

// Cli.New Creates a new plumber for pipes.
func (a *App) New(c *cli.App) *App {
	a.Cli = c

	a.Cli.Before = a.before()

	if a.Cli.Action == nil {
		a.Cli.Action = a.defaultAction()
	}

	a.Cli.Flags = append(CliDefaultFlags, a.Cli.Flags...)

	a.Environment = AppEnvironment{}

	// create error channels
	a.Control.Err = make(chan error)
	a.Control.Fatal = make(chan error, 1)
	a.Control.Interrupt = make(chan os.Signal)

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
		if a.Log == nil {
			a.Log = logger.InitiateLogger(logrus.DebugLevel)
		}

		a.Log.Fatalln(err)
	}

	return a
}

// Cli.greet Greet the user with the application name and version.
func (a *App) greet() {
	name := fmt.Sprintf("%s - %s", a.Cli.Name, a.Cli.Version)
	fmt.Println(name)
	fmt.Println(strings.Repeat("-", len(name)))
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

// Cli.before Before function for the CLI that gets executed before the action.
func (a *App) before() cli.BeforeFunc {
	return func(ctx *cli.Context) error {
		err := a.loadEnvironment()

		if err != nil {
			return err
		}

		level, err := logrus.ParseLevel(ctx.String("log_level"))

		if err != nil {
			fmt.Printf("Log level is not valid with %s, using default.\n", level)

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

		if a.Cli.Before != nil {
			if err := a.Cli.Before(ctx); err != nil {
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

		a.Log.Fatalln("Application needs a subcommand to run.")

		return nil
	}
}

// App.registerInterruptHandler Registers the os.Signal listener for the application.
func (a *App) registerInterruptHandler() {
	signal.Notify(a.Control.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	interrupt := <-a.Control.Interrupt
	a.Log.Errorf(
		"Terminating the application with operating system signal: %s",
		interrupt,
	)

	a.Terminate(1)
}

// App.registerErrorHandler Registers the error handlers for the runtime errors, this will not terminate application.
func (a *App) registerErrorHandler() {
	for {
		err := <-a.Control.Err
		if err == nil {
			return
		}
	}
}

// App.registerFatalErrorHandler Registers the error handler for fatal errors, this will terminate the application.
func (a *App) registerFatalErrorHandler() {
	err := <-a.Control.Fatal
	a.Log.Errorln(err)

	a.Terminate(127)
}

// App.Terminate Terminates the application.
func (a *App) Terminate(code int) {
	close(a.Control.Err)
	close(a.Control.Fatal)
	close(a.Control.Interrupt)

	os.Exit(code)
}
