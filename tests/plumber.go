package tests

import (
	"sync"

	"github.com/cenk1cenk2/plumber/v6"
	. "github.com/onsi/ginkgo/v2"
	"github.com/urfave/cli/v3"
)

type PlumberFixture struct {
	Plumber *plumber.Plumber

	lock  sync.Mutex
	exits []int
}

func NewPlumber(constructors ...plumber.PlumberNewFn) *PlumberFixture {
	GinkgoHelper()

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

	fixture := &PlumberFixture{
		Plumber: app,
	}

	// The application would take the whole suite down with it whenever it exits, therefore the
	// fixture records the exit codes instead of ending the process.
	app.SetExitFunc(fixture.exit)

	return fixture
}

// Returns the exit codes that the application has requested while the fixture was alive.
func (f *PlumberFixture) ExitCodes() []int {
	f.lock.Lock()
	defer f.lock.Unlock()

	return append([]int{}, f.exits...)
}

func (f *PlumberFixture) exit(code int) {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.exits = append(f.exits, code)
}

func UseGinkgoLogger(app *plumber.Plumber) *plumber.Plumber {
	GinkgoHelper()

	app.Log.SetOutput(GinkgoWriter)
	app.Log.SetLevel(plumber.LOG_LEVEL_TRACE)
	app.Log.SetReportCaller(false)

	return app
}

func (f *PlumberFixture) NewTaskList(name string) *plumber.TaskList {
	GinkgoHelper()

	tl := plumber.NewTaskList(f.Plumber)
	tl.Name = name

	return tl
}
