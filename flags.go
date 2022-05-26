package plumber

import (
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var CliDefaultFlags = []cli.Flag{
	&cli.BoolFlag{
		Name:    "ci",
		Usage:   "Sets whether this is running inside a CI/CD environment.",
		EnvVars: []string{"CI"},
	},

	&cli.BoolFlag{
		Name:    "debug",
		Usage:   "Enable debugging for the application.",
		EnvVars: []string{"DEBUG"},
	},

	&cli.StringFlag{
		Name:    "log_level",
		Usage:   "Define the log level for the application.",
		EnvVars: []string{"LOG_LEVEL"},
		Value:   logrus.InfoLevel.String(),
	},
}
