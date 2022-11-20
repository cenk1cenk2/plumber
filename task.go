package plumber

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
	"gitlab.kilic.dev/libraries/go-utils/utils"
	"golang.org/x/exp/slices"
)

type Task[Pipe TaskListData] struct {
	Plumber  *Plumber
	taskList *TaskList[Pipe]
	Log      *logrus.Entry
	Channel  *AppChannel
	Lock     *sync.RWMutex
	taskLock *sync.RWMutex
	Pipe     *Pipe
	Control  floc.Control

	Name    string
	options TaskOptions[Pipe]

	commands          []*Command[Pipe]
	parent            *Task[Pipe]
	subtask           Job
	emptyJob          Job
	fn                TaskFn[Pipe]
	shouldRunBeforeFn TaskFn[Pipe]
	shouldRunAfterFn  TaskFn[Pipe]
	onTerminatorFn    TaskFn[Pipe]
	jobWrapperFn      JobWrapperFn
}

type TaskOptions[Pipe TaskListData] struct {
	Skip    TaskPredicateFn[Pipe]
	Disable TaskPredicateFn[Pipe]
	marks   []string
}

type (
	TaskFn[Pipe TaskListData]          func(t *Task[Pipe]) error
	TaskPredicateFn[Pipe TaskListData] func(t *Task[Pipe]) bool
	JobWrapperFn                       func(job Job) Job
)

const (
	// marks.

	MARK_ROUTINE string = "MARK_ROUTINE"
)

// NewTask Creates a new task to be run as a job.
func NewTask[Pipe TaskListData](tl *TaskList[Pipe], name ...string) *Task[Pipe] {
	t := &Task[Pipe]{
		Name:     strings.Join(utils.DeleteEmptyStringsFromSlice(name), tl.Plumber.options.delimiter),
		taskList: tl,
		Plumber:  tl.Plumber,
		Lock:     tl.Lock,
		taskLock: &sync.RWMutex{},
		Channel:  tl.Channel,
		Pipe:     &tl.Pipe,
		Control:  tl.Control,
	}

	t.Log = tl.Log.WithField(LOG_FIELD_CONTEXT, t.Name)

	t.emptyJob = tl.JobIf(tl.Predicate(func(tl *TaskList[Pipe]) bool {
		return false
	}),
		func(ctx floc.Context, ctrl floc.Control) error {
			return nil
		},
	)
	t.subtask = t.emptyJob

	return t
}

// Sets the function that should run before the task.
func (t *Task[Pipe]) ShouldRunBefore(fn TaskFn[Pipe]) *Task[Pipe] {
	t.shouldRunBeforeFn = fn

	return t
}

// Sets the function that should run as task.
func (t *Task[Pipe]) Set(fn TaskFn[Pipe]) *Task[Pipe] {
	t.fn = fn

	return t
}

// Sets the function that should run after the task.
func (t *Task[Pipe]) ShouldRunAfter(fn TaskFn[Pipe]) *Task[Pipe] {
	t.shouldRunAfterFn = fn

	return t
}

// Sets the predicate that should conditionally disable the task depending on the pipe variables.
func (t *Task[Pipe]) ShouldDisable(fn TaskPredicateFn[Pipe]) *Task[Pipe] {
	t.options.Disable = fn

	return t
}

// Checks whether the current task is disabled or not.
func (t *Task[Pipe]) IsDisabled() bool {
	if t.options.Disable == nil {
		return false
	}

	return t.options.Disable(t)
}

// Sets the predicate that should conditionally skip the task depending on the pipe variables.
func (t *Task[Pipe]) ShouldSkip(fn TaskPredicateFn[Pipe]) *Task[Pipe] {
	t.options.Skip = fn

	return t
}

// Checks whether the current task is skipped or not.
func (t *Task[Pipe]) IsSkipped() bool {
	if t.options.Skip == nil {
		return false
	}

	return t.options.Skip(t)
}

// Enables global plumber terminator on this task.
func (t *Task[Pipe]) EnableTerminator() *Task[Pipe] {
	t.Log.Tracef("Registered terminator.")
	t.Plumber.RegisterTerminator()

	go t.handleTerminator()

	return t
}

// Sets the function that should fire whenever the application is globally terminated.
func (t *Task[Pipe]) SetOnTerminator(fn TaskFn[Pipe]) *Task[Pipe] {
	t.onTerminatorFn = fn

	return t
}

// Sets marks to change the behavior of the task.
func (t *Task[Pipe]) SetMarks(marks ...string) *Task[Pipe] {
	t.options.marks = marks

	return t
}

// Checks whether current task is marked as a routine that is mostly working as a async manner.
func (t *Task[Pipe]) IsMarkedAsRoutine() bool {
	return slices.Contains(t.options.marks, MARK_ROUTINE)
}

// Extend the job of the current task.
func (t *Task[Pipe]) SetJobWrapper(fn JobWrapperFn) *Task[Pipe] {
	t.jobWrapperFn = fn

	return t
}

// Runs the current task.
func (t *Task[Pipe]) Run() error {
	if stop := t.handleStopCases(); stop {
		return nil
	}

	started := time.Now()
	t.Log.WithField(LOG_FIELD_STATUS, log_status_start).Traceln("$")

	if t.shouldRunBeforeFn != nil {
		started := time.Now()
		t.Log.WithField(LOG_FIELD_STATUS, log_status_run).Traceln("$.ShouldRunBefore")
		if err := t.shouldRunBeforeFn(t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
		t.Log.WithField(LOG_FIELD_STATUS, log_status_end).Tracef("$.ShouldRunBefore -> %s", time.Since(started).Round(time.Millisecond).String())
	}

	if t.fn != nil {
		started := time.Now()
		t.Log.WithField(LOG_FIELD_STATUS, log_status_run).Traceln("$.Task")
		if err := t.fn(t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
		t.Log.WithField(LOG_FIELD_STATUS, log_status_end).Tracef("$.Task -> %s", time.Since(started).Round(time.Millisecond).String())
	}

	if t.shouldRunAfterFn != nil {
		started := time.Now()
		t.Log.WithField(LOG_FIELD_STATUS, log_status_run).Traceln("$.ShouldRunAfter")
		if err := t.shouldRunAfterFn(t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
		t.Log.WithField(LOG_FIELD_STATUS, log_status_end).Tracef("$.ShouldRunAfter -> %s", time.Since(started).Round(time.Millisecond).String())
	}

	t.Log.WithField(LOG_FIELD_STATUS, log_status_finish).Tracef("$ -> %s", time.Since(started).Round(time.Millisecond).String())

	return nil
}

// Runs the current task as a job.
func (t *Task[Pipe]) Job() Job {
	return t.taskList.JobIfNot(
		t.taskList.Predicate(func(tl *TaskList[Pipe]) bool {
			return t.IsDisabled() || t.IsSkipped()
		}),
		t.taskList.CreateJob(func(tl *TaskList[Pipe]) error {
			if t.jobWrapperFn != nil {
				return tl.RunJobs(t.jobWrapperFn(
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

// Send the error message to plumber while running inside a routine.
func (t *Task[Pipe]) SendError(err error) *Task[Pipe] {
	t.Plumber.SendCustomError(t.Log, err)

	return t
}

// Send the fatal error message to plumber while running inside a routine.
func (t *Task[Pipe]) SendFatal(err error) *Task[Pipe] {
	t.Control.Cancel(err)
	t.Plumber.SendCustomFatal(t.Log, err)

	return t
}

// Trigger the exit protocol of plumber.
func (t *Task[Pipe]) SendExit(code int) *Task[Pipe] {
	t.Control.Cancel(fmt.Sprintf("Will exit with code: %d", code))
	t.Plumber.SendExit(code)

	return t
}

// Handles the stop cases of the task.
func (t *Task[Pipe]) handleStopCases() bool {
	if result := t.IsDisabled(); result {
		t.Log.WithField(LOG_FIELD_CONTEXT, log_context_disable).
			Debugf("%s", t.Name)

		return true
	} else if result := t.IsSkipped(); result {
		t.Log.WithField(LOG_FIELD_CONTEXT, log_context_skipped).
			Warnf("%s", t.Name)

		return true
	}

	return false
}

// Handles the errors from the current task.
func (t *Task[Pipe]) handleErrors(err error) error {
	if t.IsMarkedAsRoutine() {
		t.SendFatal(err)

		return nil
	}

	return err
}

// Handles the plumber terminator when terminator is triggered.
func (t *Task[Pipe]) handleTerminator() {
	ch := make(chan os.Signal, 1)

	t.Plumber.Terminator.ShouldTerminate.Register(ch)

	sig := <-ch

	if t.IsDisabled() || t.IsSkipped() {
		t.Log.Traceln("Sending terminated directly because the task is already not available.")

		t.Plumber.RegisterTerminated()

		return
	}

	t.Log.Tracef("Forwarding signal to task: %s", sig)

	if t.onTerminatorFn != nil {
		t.SendError(t.onTerminatorFn(t))
	}

	t.Log.Tracef("Registered as terminated.")
	t.Plumber.RegisterTerminated()
}
