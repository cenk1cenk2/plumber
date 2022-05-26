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
	Environment struct {
		Debug bool
		CI    bool
	}
}

// Cli.New Creates a new plumber for pipes.
func (c *App) New(app *cli.App) *App {
	c.Cli = app

	c.Cli.Before = c.before(c.Cli.Before)

	c.Cli.Flags = append(CliDefaultFlags, c.Cli.Flags...)

	return c
}

// Cli.Run Starts the application.
func (c *App) Run() {
	c.greet()

	if err := c.Cli.Run(os.Args); err != nil {
		if c.Log == nil {
			c.Log = logger.InitiateLogger(logrus.DebugLevel)
		}

		c.Log.Fatalln(err)
	}
}

// Cli.greet Greet the user with the application name and version.
func (c *App) greet() {
	name := fmt.Sprintf("%s - %s", c.Cli.Name, c.Cli.Version)
	fmt.Println(name)
	fmt.Println(strings.Repeat("-", len(name)))
}

// Cli.loadEnvironment Loads the given environment file to the application.
func (c *App) loadEnvironment() error {
	if env := os.Getenv("ENV_FILE"); env != "" {
		if err := godotenv.Load(env); err != nil {
			return err
		}
	}

	return nil
}

// Cli.before Before function for the CLI that gets executed before the action.
func (c *App) before(b cli.BeforeFunc) cli.BeforeFunc {
	return func(ctx *cli.Context) error {
		err := c.loadEnvironment()

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

			c.Environment.Debug = true
		}

		if ctx.Bool("ci") {
			c.Environment.CI = true
		}

		c.Log = logger.InitiateLogger(level)

		if b != nil {
			if err := b(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}
