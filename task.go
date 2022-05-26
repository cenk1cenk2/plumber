package plumber

import (
	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
)

type Task[Ctx struct{}] struct {
	Name    string
	Options TaskOptions[Ctx]

	App     *App
	Log     *logrus.Entry
	Context *Ctx

	tl       *TaskList[Ctx]
	commands []Command
	fn       task[Ctx]
}

type TaskOptions[Ctx struct{}] struct {
	Skip    func(t *TaskList[Ctx]) bool
	Disable func(t *TaskList[Ctx]) bool
}

type (
	task[Ctx struct{}] func(*Task[Ctx]) error
)

func (t *Task[Ctx]) New(tl *TaskList[Ctx], name string, options TaskOptions[Ctx]) *Task[Ctx] {
	t.Name = name
	t.Options = options
	t.commands = []Command{}

	if t.Options.Skip == nil {
		t.Options.Skip = func(t *TaskList[Ctx]) bool {
			return false
		}
	}

	if t.Options.Disable == nil {
		t.Options.Disable = func(t *TaskList[Ctx]) bool {
			return false
		}
	}

	t.tl = tl
	t.Log = tl.Log.WithField("context", t.Name)
	t.Context = &tl.Context
	t.App = tl.App

	return t
}

func (t *Task[Ctx]) Set(fn task[Ctx]) *Task[Ctx] {
	t.fn = fn

	return t
}

func (t *Task[Ctx]) AddCommands(commands ...Command) *Task[Ctx] {
	t.commands = append(t.commands, commands...)

	return t
}

func (t *Task[Ctx]) Run() floc.Job {
	return func(ctx floc.Context, ctrl floc.Control) error {

		return nil
	}
}
