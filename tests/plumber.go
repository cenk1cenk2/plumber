package tests

import (
	"github.com/cenk1cenk2/plumber/v6"
	"github.com/cenk1cenk2/plumber/v6/logger"
	. "github.com/onsi/ginkgo/v2"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

type PlumberFixture struct {
	Plumber *plumber.Plumber
}

func NewPlumber(constructors ...plumber.PlumberNewFn) *PlumberFixture {
	GinkgoHelper()

	previousLogger := logger.Log
	logger.Log = nil

	DeferCleanup(func() {
		logger.Log = previousLogger
	})

	constructor := func(_ *plumber.Plumber) *cli.Command {
		return &cli.Command{
			Name:    "plumber-test",
			Version: "test",
		}
	}

	if len(constructors) > 0 && constructors[0] != nil {
		constructor = constructors[0]
	}

	app := plumber.NewPlumber(constructor)

	app.DisableGreeter()
	UseGinkgoLogger(app)

	return &PlumberFixture{
		Plumber: app,
	}
}

func UseGinkgoLogger(app *plumber.Plumber) *plumber.Plumber {
	GinkgoHelper()

	app.Log.SetOutput(GinkgoWriter)
	app.Log.SetLevel(logrus.TraceLevel)
	app.Log.SetReportCaller(false)

	return app
}

func (f *PlumberFixture) NewTaskList(name string) *plumber.TaskList {
	GinkgoHelper()

	tl := plumber.NewTaskList(f.Plumber)
	tl.Name = name

	return tl
}
