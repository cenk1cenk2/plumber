package plumber

import (
	"github.com/sirupsen/logrus"
)

type Task[Ctx struct{}] struct {
	Name     string
	Options  TaskOptions[Ctx]
	taskList TaskList[Ctx]
	Log      *logrus.Entry
	Context  *Ctx
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

	t.Log = t.taskList.Log.WithField("context", t.Name)
	t.Context = &t.taskList.Context

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

func (t *Task[Ctx]) Run() error {
	return nil
}
