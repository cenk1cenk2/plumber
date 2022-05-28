package plumber

import (
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
)

type Task[Pipe TaskListData, Ctx TaskListData] struct {
	Name    string
	options TaskOptions[Pipe, Ctx]

	App     *App
	Log     *logrus.Entry
	Channel *AppChannel
	Lock    *sync.RWMutex

	Pipe    *Pipe
	Context *Ctx
	Control floc.Control

	taskList *TaskList[Pipe, Ctx]

	commands []Command
	fn       taskFn[Pipe, Ctx]
}

type TaskOptions[Pipe TaskListData, Ctx TaskListData] struct {
	Skip    taskPredicateFn[Pipe, Ctx]
	Disable taskPredicateFn[Pipe, Ctx]
}

type (
	taskFn[Pipe TaskListData, Ctx TaskListData]          func(*Task[Pipe, Ctx], floc.Control) error
	taskPredicateFn[Pipe TaskListData, Ctx TaskListData] func(*Task[Pipe, Ctx]) bool
)

func (t *Task[Pipe, Ctx]) New(taskList *TaskList[Pipe, Ctx], name string) *Task[Pipe, Ctx] {
	t.Name = name
	t.options = TaskOptions[Pipe, Ctx]{
		Skip: func(t *Task[Pipe, Ctx]) bool {
			return false
		},
		Disable: func(t *Task[Pipe, Ctx]) bool {
			return false
		},
	}
	t.commands = []Command{}

	t.taskList = taskList

	t.App = taskList.App
	t.Log = taskList.Log.WithField("context", t.Name)
	t.Lock = taskList.Lock
	t.Channel = taskList.Channnel

	t.Context = taskList.Context
	t.Pipe = taskList.Pipe
	t.Control = taskList.Control

	return t
}

func (t *Task[Pipe, Ctx]) Set(fn taskFn[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.fn = fn

	return t
}

func (t *Task[Pipe, Ctx]) ShouldDisable(fn taskPredicateFn[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.options.Disable = fn

	return t
}

func (t *Task[Pipe, Ctx]) ShouldSkip(fn taskPredicateFn[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.options.Skip = fn

	return t
}

func (t *Task[Pipe, Ctx]) AddCommands(commands ...Command) *Task[Pipe, Ctx] {
	t.commands = append(t.commands, commands...)

	return t
}

func (t *Task[Pipe, Ctx]) Run() error {
	if result := t.options.Disable(t); result {
		t.Log.WithField("context", "DISABLE").
			Debugf("%s", t.Name)

		return nil
	} else if result := t.options.Skip(t); result {
		t.Log.WithField("context", "SKIP").
			Warnf("%s", t.Name)

		return nil
	}

	return t.fn(t, t.Control)
}

func (t *Task[Pipe, Ctx]) Job() floc.Job {
	return func(ctx floc.Context, ctrl floc.Control) error {
		return t.Run()
	}
}
