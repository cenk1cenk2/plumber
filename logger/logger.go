package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

// Log Returns a new logrus logger instance.
var Log *logrus.Logger

// InitiateLogger the default logger.
func InitiateLogger(level logrus.Level) *logrus.Logger {
	if Log != nil {
		return Log
	}

	Log = logrus.New()

	Log.Out = os.Stdout

	Log.SetFormatter(&Formatter{
		FieldsOrder:      []string{"context"},
		TimestampFormat:  "",
		HideKeys:         true,
		NoColors:         false,
		NoFieldsColors:   false,
		NoFieldsSpace:    false,
		NoEmptyFields:    true,
		ShowFullLevel:    false,
		NoUppercaseLevel: false,
		TrimMessages:     false,
		CallerFirst:      true,
	})

	Log.SetLevel(level)

	return Log
}
