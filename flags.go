package plumber

import (
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

const CLI_FLAGS_CATEGORY = "CLI"

// flags for a Plumber application.
var CliDefaultFlags = []cli.Flag{
	&cli.BoolFlag{
		Category: CLI_FLAGS_CATEGORY,
		Name:     "ci",
		Usage:    "Sets whether this is running inside a CI/CD environment.",
		Hidden:   true,
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("CI"),
		),
	},

	&cli.BoolFlag{
		Category: CLI_FLAGS_CATEGORY,
		Name:     "debug",
		Usage:    "Enable debugging for the application.",
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("DEBUG"),
		),
		Hidden: true,
	},

	&cli.StringFlag{
		Category: CLI_FLAGS_CATEGORY,
		Name:     "log-level",
		Usage:    `Define the log level for the application. enum("panic", "fatal", "warn", "info", "debug", "trace")`,
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("LOG_LEVEL"),
		),
		Value: logrus.InfoLevel.String(),
	},

	&cli.StringSliceFlag{
		Category: CLI_FLAGS_CATEGORY,
		Name:     "env-file",
		Usage:    "Environment files to inject.",
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("ENV_FILE"),
		),
	},
}
