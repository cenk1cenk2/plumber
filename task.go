package plumber

import (
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
)

type Task[Pipe TaskListData] struct {
	Name    string
	options TaskOptions[Pipe]

	Plumber *Plumber
	Log     *logrus.Entry
	Channel *AppChannel
	Lock    *sync.RWMutex

	Pipe    *Pipe
	Control floc.Control

	taskList *TaskList[Pipe]

	subtask   floc.Job
	emptyJob  floc.Job
	commands  *[]Command[Pipe]
	fn        taskFn[Pipe]
	runBefore taskFn[Pipe]
	runAfter  taskFn[Pipe]
}

type TaskOptions[Pipe TaskListData] struct {
	Skip    taskPredicateFn[Pipe]
	Disable taskPredicateFn[Pipe]
}

type (
	taskFn[Pipe TaskListData]          func(*Task[Pipe], floc.Control) error
	taskPredicateFn[Pipe TaskListData] func(*Task[Pipe]) bool
)

const (
	task_disabled = "OFF"
	task_skipped  = "SKIP"
)

func (t *Task[Pipe]) New(tl *TaskList[Pipe], name string) *Task[Pipe] {
	t.Name = name
	t.options = TaskOptions[Pipe]{
		Skip: func(t *Task[Pipe]) bool {
			return false
		},
		Disable: func(t *Task[Pipe]) bool {
			return false
		},
	}
	t.commands = &[]Command[Pipe]{}

	t.runBefore = func(tl *Task[Pipe], c floc.Control) error {
		return nil
	}
	t.runAfter = func(tl *Task[Pipe], c floc.Control) error {
		return nil
	}

	t.taskList = tl

	t.Plumber = tl.Plumber
	t.Log = tl.Log.WithField("context", t.Name)
	t.Lock = tl.Lock
	t.Channel = tl.Channel

	t.emptyJob = tl.JobIf(tl.Predicate(func(tl *TaskList[Pipe]) bool {
		return false
	}),
		func(ctx floc.Context, ctrl floc.Control) error {
			return nil
		},
	)
	t.subtask = t.emptyJob

	t.Pipe = &tl.Pipe
	t.Control = tl.Control

	return t
}

func (t *Task[Pipe]) Set(fn taskFn[Pipe]) *Task[Pipe] {
	t.fn = fn

	return t
}

func (t *Task[Pipe]) CreateSubtask(name string) *Task[Pipe] {
	st := &Task[Pipe]{}

	if name == "" {
		name = t.Name
	}

	return st.New(t.taskList, name)
}

func (t *Task[Pipe]) ToParent(
	parent *Task[Pipe],
	fn func(pt *Task[Pipe], st *Task[Pipe]),
) *Task[Pipe] {
	fn(parent, t)

	return t
}

func (t *Task[Pipe]) SetSubtask(job floc.Job) *Task[Pipe] {
	t.subtask = job

	return t
}

func (t *Task[Pipe]) ExtendSubtask(fn func(floc.Job) floc.Job) *Task[Pipe] {
	t.subtask = fn(t.subtask)

	return t
}

func (t *Task[Pipe]) GetSubtasks() floc.Job {
	return t.subtask
}

func (t *Task[Pipe]) RunSubtasks() error {
	err := t.taskList.RunJobs(t.subtask)

	if err != nil {
		t.SetSubtask(t.emptyJob)
	}

	return err
}

func (t *Task[Pipe]) ShouldDisable(fn taskPredicateFn[Pipe]) *Task[Pipe] {
	t.options.Disable = fn

	return t
}

func (t *Task[Pipe]) ShouldSkip(fn taskPredicateFn[Pipe]) *Task[Pipe] {
	t.options.Skip = fn

	return t
}

func (t *Task[Pipe]) ShouldRunBefore(fn taskFn[Pipe]) *Task[Pipe] {
	t.runBefore = fn

	return t
}

func (t *Task[Pipe]) ShouldRunAfter(fn taskFn[Pipe]) *Task[Pipe] {
	t.runAfter = fn

	return t
}

func (t *Task[Pipe]) CreateCommand(command string, args ...string) *Command[Pipe] {
	cmd := Command[Pipe]{}

	return cmd.New(t, command, args...)
}

func (t *Task[Pipe]) AddCommands(commands ...*Command[Pipe]) *Task[Pipe] {
	for _, v := range commands {
		*t.commands = append(*t.commands, *v)
	}

	return t
}

func (t *Task[Pipe]) GetCommands() *[]Command[Pipe] {
	return t.commands
}

func (t *Task[Pipe]) GetCommandJobs() []floc.Job {
	jobs := []floc.Job{}
	for _, v := range *t.commands {
		jobs = append(jobs, v.Job())
	}

	return jobs
}

func (t *Task[Pipe]) GetCommandJobAsJobSequence() floc.Job {
	jobs := t.GetCommandJobs()

	if len(jobs) == 0 {
		return nil
	}

	return t.taskList.JobSequence(jobs...)
}

func (t *Task[Pipe]) RunCommandJobAsJobSequence() error {
	return t.taskList.RunJobs(t.GetCommandJobAsJobSequence())
}

func (t *Task[Pipe]) GetCommandJobAsJobParallel() floc.Job {
	jobs := t.GetCommandJobs()

	if len(jobs) == 0 {
		return nil
	}

	return t.taskList.JobParallel(jobs...)
}

func (t *Task[Pipe]) RunCommandJobAsJobParallel() error {
	return t.taskList.RunJobs(t.GetCommandJobAsJobParallel())
}

func (t *Task[Pipe]) Run() error {
	if result := t.options.Disable(t); result {
		t.Log.WithField("context", task_disabled).
			Debugf("%s", t.Name)

		return nil
	} else if result := t.options.Skip(t); result {
		t.Log.WithField("context", task_skipped).
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

func (t *Task[Pipe]) Job() floc.Job {
	return func(ctx floc.Context, ctrl floc.Control) error {
		return t.Run()
	}
}
