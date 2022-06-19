package plumber

import (
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
)

type Task[Pipe TaskListData] struct {
	Name    string
	options TaskOptions[Pipe]

	Plumber  *Plumber
	Log      *logrus.Entry
	Channel  *AppChannel
	Lock     *sync.RWMutex
	taskLock *sync.RWMutex

	Pipe    *Pipe
	Control floc.Control

	taskList *TaskList[Pipe]

	subtask    Job
	emptyJob   Job
	parent     *Task[Pipe]
	commands   []*Command[Pipe]
	fn         TaskFn[Pipe]
	runBefore  TaskFn[Pipe]
	runAfter   TaskFn[Pipe]
	jobWrapper JobWrapperFn
}

type TaskOptions[Pipe TaskListData] struct {
	Skip    TaskPredicateFn[Pipe]
	Disable TaskPredicateFn[Pipe]
}

type (
	TaskFn[Pipe TaskListData]          func(t *Task[Pipe]) error
	TaskPredicateFn[Pipe TaskListData] func(t *Task[Pipe]) bool
	JobWrapperFn                       func(job Job) Job
)

const (
	task_disabled = "DISABLE"
	task_skipped  = "SKIP"
)

func NewTask[Pipe TaskListData](tl *TaskList[Pipe], name string) *Task[Pipe] {
	t := &Task[Pipe]{}

	t.Name = name
	t.options = TaskOptions[Pipe]{
		Skip: func(t *Task[Pipe]) bool {
			return false
		},
		Disable: func(t *Task[Pipe]) bool {
			return false
		},
	}
	t.commands = []*Command[Pipe]{}

	t.taskList = tl

	t.Plumber = tl.Plumber
	t.Log = tl.Log.WithField(LOG_FIELD_CONTEXT, t.Name)
	t.Lock = tl.Lock
	t.taskLock = &sync.RWMutex{}
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

func (t *Task[Pipe]) Set(fn TaskFn[Pipe]) *Task[Pipe] {
	t.fn = fn

	return t
}

func (t *Task[Pipe]) CreateSubtask(name string) *Task[Pipe] {
	st := NewTask(t.taskList, name)

	st.parent = t

	if name == "" {
		st.Name = t.Name
	}

	return st
}

func (t *Task[Pipe]) ToParent(
	parent *Task[Pipe],
	fn func(pt *Task[Pipe], st *Task[Pipe]),
) *Task[Pipe] {
	t.parent.taskLock.Lock()
	fn(parent, t)
	t.parent.taskLock.Unlock()

	return t
}

func (t *Task[Pipe]) HasParent() bool {
	return t.parent != nil
}

func (t *Task[Pipe]) AddSelfToParent(
	fn func(pt *Task[Pipe], st *Task[Pipe]),
) *Task[Pipe] {
	if !t.HasParent() {
		t.Plumber.SendCustomFatal(t.Log, fmt.Errorf("Task has no parent value set."))

		return t
	}

	t.parent.Lock.Lock()
	fn(t.parent, t)
	t.parent.Lock.Unlock()

	return t
}

func (t *Task[Pipe]) SetSubtask(job Job) *Task[Pipe] {
	t.taskLock.Lock()
	t.subtask = job
	t.taskLock.Unlock()

	return t
}

func (t *Task[Pipe]) ExtendSubtask(fn JobWrapperFn) *Task[Pipe] {
	t.taskLock.Lock()
	t.subtask = fn(t.subtask)
	t.taskLock.Unlock()

	return t
}

func (t *Task[Pipe]) GetSubtasks() Job {
	return t.subtask
}

func (t *Task[Pipe]) RunSubtasks() error {
	err := t.taskList.RunJobs(t.subtask)

	if err == nil {
		t.SetSubtask(t.emptyJob)
	}

	return err
}

func (t *Task[Pipe]) RunSubtasksWithExtension(fn func(job Job) Job) error {
	t.subtask = fn(t.subtask)

	return t.RunSubtasks()
}

func (t *Task[Pipe]) SetJobWrapper(fn JobWrapperFn) *Task[Pipe] {
	t.jobWrapper = fn

	return t
}

func (t *Task[Pipe]) ShouldDisable(fn TaskPredicateFn[Pipe]) *Task[Pipe] {
	t.options.Disable = fn

	return t
}

func (t *Task[Pipe]) ShouldSkip(fn TaskPredicateFn[Pipe]) *Task[Pipe] {
	t.options.Skip = fn

	return t
}

func (t *Task[Pipe]) ShouldRunBefore(fn TaskFn[Pipe]) *Task[Pipe] {
	t.runBefore = fn

	return t
}

func (t *Task[Pipe]) ShouldRunAfter(fn TaskFn[Pipe]) *Task[Pipe] {
	t.runAfter = fn

	return t
}

func (t *Task[Pipe]) SendError(err error) *Task[Pipe] {
	t.Plumber.SendCustomError(t.Log, err)

	return t
}

func (t *Task[Pipe]) SendFatal(err error) *Task[Pipe] {
	t.Plumber.SendCustomFatal(t.Log, err)

	return t
}

func (t *Task[Pipe]) CreateCommand(command string, args ...string) *Command[Pipe] {
	return NewCommand(t, command, args...)
}

func (t *Task[Pipe]) AddCommands(commands ...*Command[Pipe]) *Task[Pipe] {
	t.taskLock.Lock()
	t.commands = append(t.commands, commands...)
	t.taskLock.Unlock()

	return t
}

func (t *Task[Pipe]) GetCommands() []*Command[Pipe] {
	return t.commands
}

func (t *Task[Pipe]) GetCommandJobs() []Job {
	j := []Job{}
	for _, c := range t.commands {
		j = append(j, c.Job())
	}

	return j
}

func (t *Task[Pipe]) GetCommandJobAsJobSequence() Job {
	j := t.GetCommandJobs()

	if len(j) == 0 {
		return nil
	}

	return t.taskList.JobSequence(j...)
}

func (t *Task[Pipe]) GetCommandJobAsJobParallel() Job {
	j := t.GetCommandJobs()

	if len(j) == 0 {
		return nil
	}

	return t.taskList.JobParallel(j...)
}

func (t *Task[Pipe]) RunCommandJobAsJobSequence() error {
	return t.taskList.RunJobs(t.GetCommandJobAsJobSequence())
}

func (t *Task[Pipe]) RunCommandJobAsJobSequenceWithExtension(fn JobWrapperFn) error {
	return t.taskList.RunJobs(fn(t.GetCommandJobAsJobSequence()))
}

func (t *Task[Pipe]) RunCommandJobAsJobParallel() error {
	return t.taskList.RunJobs(t.GetCommandJobAsJobParallel())
}

func (t *Task[Pipe]) RunCommandJobAsJobParallelWithExtension(fn JobWrapperFn) error {
	return t.taskList.RunJobs(fn(t.GetCommandJobAsJobParallel()))
}

func (t *Task[Pipe]) Run() error {
	if stop := t.handleStopCases(); stop {
		return nil
	}

	if t.runBefore != nil {
		if err := t.runBefore(t); err != nil {
			t.Log.Errorln(err)
			return err
		}
	}

	if err := t.fn(t); err != nil {
		t.Log.Errorln(err)
		return err
	}

	if t.runAfter != nil {
		if err := t.runAfter(t); err != nil {
			t.Log.Errorln(err)
			return err
		}
	}

	return nil
}

func (t *Task[Pipe]) handleStopCases() bool {
	if result := t.options.Disable(t); result {
		t.Log.WithField(LOG_FIELD_CONTEXT, task_disabled).
			Debugf("%s", t.Name)

		return true
	} else if result := t.options.Skip(t); result {
		t.Log.WithField(LOG_FIELD_CONTEXT, task_skipped).
			Warnf("%s", t.Name)

		return true
	}

	return false
}

func (t *Task[Pipe]) Job() Job {
	return t.taskList.JobIfNot(
		t.taskList.Predicate(func(tl *TaskList[Pipe]) bool {
			return t.options.Disable(t) || t.options.Skip(t)
		}),
		t.taskList.CreateJob(func(tl *TaskList[Pipe]) error {
			if t.jobWrapper != nil {
				return tl.RunJobs(t.jobWrapper(
					tl.CreateBasicJob(t.Run),
				))
			}

			return t.Run()
		}),
		t.taskList.CreateJob(func(tl *TaskList[Pipe]) error {
			t.handleStopCases()

			return nil
		}),
	)
}
