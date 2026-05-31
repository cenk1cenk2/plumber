package tests

import (
	"bytes"

	"github.com/cenk1cenk2/plumber/v6"
	"github.com/cenk1cenk2/plumber/v6/logger"
	. "github.com/onsi/ginkgo/v2"
	"github.com/urfave/cli/v3"
)

type PlumberFixture struct {
	Plumber *plumber.Plumber
	Output  *bytes.Buffer
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
	output := &bytes.Buffer{}

	app.DisableGreeter()
	app.Log.SetOutput(output)
	app.Log.SetReportCaller(false)

	return &PlumberFixture{
		Plumber: app,
		Output:  output,
	}
}

func (f *PlumberFixture) NewTaskList(name string) *plumber.TaskList {
	GinkgoHelper()

	tl := plumber.NewTaskList(f.Plumber)
	tl.Name = name

	return tl
}
