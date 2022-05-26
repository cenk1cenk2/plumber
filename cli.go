package plumber

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gitlab.kilic.dev/libraries/go-utils/logger"
)

type App struct {
	Cli         *cli.App
	Log         *logrus.Logger
	Environment AppEnvironment
}

type AppEnvironment struct {
	Debug bool
	CI    bool
}

// Cli.New Creates a new plumber for pipes.
func (a *App) New(c *cli.App) *App {
	a.Cli = c

	a.Cli.Before = a.before()

	a.Cli.Flags = append(CliDefaultFlags, a.Cli.Flags...)

	a.Environment = AppEnvironment{}

	return a
}

// Cli.Run Starts the application.
func (a *App) Run() *App {
	a.greet()

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
