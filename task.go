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

	TaskList *TaskList[Pipe, Ctx]

	subtasks  floc.Job
	commands  []Command[Pipe, Ctx]
	fn        taskFn[Pipe, Ctx]
	runBefore taskFn[Pipe, Ctx]
	runAfter  taskFn[Pipe, Ctx]
}

type TaskOptions[Pipe TaskListData, Ctx TaskListData] struct {
	Skip    taskPredicateFn[Pipe, Ctx]
	Disable taskPredicateFn[Pipe, Ctx]
}

type (
	taskFn[Pipe TaskListData, Ctx TaskListData]          func(*Task[Pipe, Ctx], floc.Control) error
	taskPredicateFn[Pipe TaskListData, Ctx TaskListData] func(*Task[Pipe, Ctx]) bool
)

func (t *Task[Pipe, Ctx]) New(tl *TaskList[Pipe, Ctx], name string) *Task[Pipe, Ctx] {
	t.Name = name
	t.options = TaskOptions[Pipe, Ctx]{
		Skip: func(t *Task[Pipe, Ctx]) bool {
			return false
		},
		Disable: func(t *Task[Pipe, Ctx]) bool {
			return false
		},
	}
	t.commands = []Command[Pipe, Ctx]{}

	t.runBefore = func(tl *Task[Pipe, Ctx], c floc.Control) error {
		return nil
	}
	t.runAfter = func(tl *Task[Pipe, Ctx], c floc.Control) error {
		return nil
	}

	t.TaskList = tl

	t.App = tl.App
	t.Log = tl.Log.WithField("context", t.Name)
	t.Lock = tl.Lock
	t.Channel = tl.Channel

	t.Context = &tl.Context
	t.Pipe = &tl.Pipe
	t.Control = tl.Control

	return t
}

func (t *Task[Pipe, Ctx]) Set(fn taskFn[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.fn = fn

	return t
}

func (t *Task[Pipe, Ctx]) SetSubtasks(fn func(floc.Job) floc.Job) *Task[Pipe, Ctx] {
	t.subtasks = fn(t.subtasks)

	return t
}

func (t *Task[Pipe, Ctx]) GetSubtasks() floc.Job {
	return t.subtasks
}

func (t *Task[Pipe, Ctx]) RunSubtasks() error {
	err := t.TaskList.RunJobs(t.subtasks)

	t.SetSubtasks(nil)

	return err
}

func (t *Task[Pipe, Ctx]) ShouldDisable(fn taskPredicateFn[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.options.Disable = fn

	return t
}

func (t *Task[Pipe, Ctx]) ShouldSkip(fn taskPredicateFn[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.options.Skip = fn

	return t
}

func (t *Task[Pipe, Ctx]) ShouldRunBefore(fn taskFn[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.runBefore = fn

	return t
}

func (t *Task[Pipe, Ctx]) ShouldRunAfter(fn taskFn[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.runAfter = fn

	return t
}

func (t *Task[Pipe, Ctx]) AddCommands(commands ...Command[Pipe, Ctx]) *Task[Pipe, Ctx] {
	t.commands = append(t.commands, commands...)

	return t
}

func (t *Task[Pipe, Ctx]) GetCommands() []Command[Pipe, Ctx] {
	return t.commands
}

func (t *Task[Pipe, Ctx]) GetCommandJobs() []floc.Job {
	jobs := []floc.Job{}
	for _, v := range t.commands {
		jobs = append(jobs, v.Job())
	}

	return jobs
}

func (t *Task[Pipe, Ctx]) GetCommandJobAsJobSequence() floc.Job {
	return t.TaskList.JobSequence(t.GetCommandJobs()...)
}

func (t *Task[Pipe, Ctx]) GetCommandJobAsJobParallel() floc.Job {
	return t.TaskList.JobParallel(t.GetCommandJobs()...)
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

	if err := t.runBefore(t, t.Control); err != nil {
		return err
	}

	if err := t.fn(t, t.Control); err != nil {
		return err
	}

	if err := t.runAfter(t, t.Control); err != nil {
		return err
	}

	return nil
}

func (t *Task[Pipe, Ctx]) Job() floc.Job {
	return func(ctx floc.Context, ctrl floc.Control) error {
		return t.Run()
	}
}
