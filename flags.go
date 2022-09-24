package plumber

import (
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var CliDefaultFlags = []cli.Flag{
	&cli.BoolFlag{
		Name:    "ci",
		Usage:   "Sets whether this is running inside a CI/CD environment.",
		Hidden:  true,
		EnvVars: []string{"CI"},
	},

	&cli.BoolFlag{
		Name:    "debug",
		Usage:   "Enable debugging for the application.",
		EnvVars: []string{"DEBUG"},
	},

	&cli.StringFlag{
		Name:    "log-level",
		Usage:   `Define the log level for the application. format(enum("PANIC", "FATAL", "WARNING", "INFO", "DEBUG", "TRACE"))`,
		EnvVars: []string{"LOG_LEVEL"},
		Value:   logrus.InfoLevel.String(),
	},
}
