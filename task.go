package plumber

import (
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
)

type Task[Pipe TaskListStore, Ctx TaskListStore] struct {
	Name    string
	Options TaskOptions[Pipe, Ctx]

	App     *App
	Log     *logrus.Entry
	Control *AppControl
	Lock    *sync.RWMutex

	Pipe    *Pipe
	Context *Ctx
	Floc    floc.Control

	TaskList *TaskList[Pipe, Ctx]
	commands []Command
	fn       task[Pipe, Ctx]
}

type TaskOptions[Pipe TaskListStore, Ctx TaskListStore] struct {
	Skip    func(t *Task[Pipe, Ctx]) bool
	Disable func(t *Task[Pipe, Ctx]) bool
}

type (
	task[Pipe TaskListStore, Ctx TaskListStore] func(*Task[Pipe, Ctx], floc.Control) error
)

func (t *Task[Pipe, Ctx]) New(taskList *TaskList[Pipe, Ctx], name string, options TaskOptions[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.Name = name
	t.Options = options
	t.commands = []Command{}

	if t.Options.Skip == nil {
		t.Options.Skip = func(t *Task[Pipe, Ctx]) bool {
			return false
		}
	}

	if t.Options.Disable == nil {
		t.Options.Disable = func(t *Task[Pipe, Ctx]) bool {
			return false
		}
	}

	t.TaskList = taskList

	t.App = taskList.App
	t.Log = taskList.Log.WithField("context", t.Name)
	t.Lock = taskList.Lock
	t.Control = taskList.Control

	t.Context = taskList.Context
	t.Pipe = taskList.Pipe
	t.Floc = taskList.Floc

	return t
}

func (t *Task[Pipe, Ctx]) Set(fn task[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.fn = fn

	return t
}

func (t *Task[Pipe, Ctx]) AddCommands(commands ...Command) *Task[Pipe, Ctx] {
	t.commands = append(t.commands, commands...)

	return t
}

func (t *Task[Pipe, Ctx]) Run() error {
	if result := t.Options.Disable(t); result {
		t.Log.WithField("context", "DISABLE").
			Debugf("%s", t.Name)

		return nil
	} else if result := t.Options.Skip(t); result {
		t.Log.WithField("context", "SKIP").
			Warnf("%s", t.Name)

		return nil
	}

	return t.fn(t, t.Floc)
}

func (t *Task[Pipe, Ctx]) Job() floc.Job {
	return func(ctx floc.Context, ctrl floc.Control) error {
		return t.Run()
	}
}
