package plumber

import (
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

const CLI_FLAGS_CATEGORY = "CLI"

// flags for a Plumber application.
var CliDefaultFlags = []cli.Flag{
	&cli.BoolFlag{
		Category: CLI_FLAGS_CATEGORY,
		Name:     "ci",
		Usage:    "Sets whether this is running inside a CI/CD environment.",
		Hidden:   true,
		EnvVars:  []string{"CI"},
	},

	&cli.BoolFlag{
		Category: CLI_FLAGS_CATEGORY,
		Name:     "debug",
		Usage:    "Enable debugging for the application.",
		EnvVars:  []string{"DEBUG"},
		Hidden:   true,
	},

	&cli.StringFlag{
		Category: CLI_FLAGS_CATEGORY,
		Name:     "log-level",
		Usage:    `Define the log level for the application. enum("panic", "fatal", "warn", "info", "debug", "trace")`,
		EnvVars:  []string{"LOG_LEVEL"},
		Value:    logrus.InfoLevel.String(),
	},

	&cli.StringSliceFlag{
		Category: CLI_FLAGS_CATEGORY,
		Name:     "env-file",
		Usage:    "Environment files to inject.",
		EnvVars:  []string{"ENV_FILE"},
	},
}
