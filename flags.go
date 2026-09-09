package plumber

import (
	"github.com/urfave/cli/v3"
)

const CliFlagsCategory = "CLI"

// flags for a Plumber application.
var CliDefaultFlags = []cli.Flag{
	&cli.BoolFlag{
		Category: CliFlagsCategory,
		Name:     "ci",
		Usage:    "Sets whether this is running inside a CI/CD environment.",
		Hidden:   true,
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("CI"),
		),
	},

	&cli.BoolFlag{
		Category: CliFlagsCategory,
		Name:     "debug",
		Usage:    "Enable debugging for the application.",
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("DEBUG"),
		),
		Hidden: true,
	},

	&cli.StringFlag{
		Category: CliFlagsCategory,
		Name:     "log-level",
		Usage:    `Define the log level for the application. enum("panic", "fatal", "warn", "info", "debug", "trace")`,
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("LOG_LEVEL"),
		),
		Value: LogLevelInfo.String(),
	},

	&cli.StringSliceFlag{
		Category: CliFlagsCategory,
		Name:     "env-file",
		Usage:    "Environment files to inject.",
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("ENV_FILE"),
		),
	},
}
