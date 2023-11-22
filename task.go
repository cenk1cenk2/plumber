package plumber

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
	"gitlab.kilic.dev/libraries/go-utils/v2/utils"
)

type Task[Pipe TaskListData] struct {
	Plumber *Plumber
	TL      *TaskList[Pipe]
	Log     *logrus.Entry
	Channel *AppChannel
	Pipe    *Pipe
	Control floc.Control
	Name    string

	Lock     *sync.RWMutex
	taskLock *sync.RWMutex

	options           TaskOptions[Pipe]
	commands          []*Command[Pipe]
	parent            *Task[Pipe]
	subtask           Job
	emptyJob          Job
	fn                TaskFn[Pipe]
	shouldRunBeforeFn TaskFn[Pipe]
	shouldRunAfterFn  TaskFn[Pipe]
	onTerminatorFn    TaskFn[Pipe]
	jobWrapperFn      TaskJobWrapperFn[Pipe]
	status            TaskStatus
}

type TaskOptions[Pipe TaskListData] struct {
	skipPredicateFn    TaskPredicateFn[Pipe]
	disablePredicateFn TaskPredicateFn[Pipe]
}

type TaskStatus struct {
	stopCases StatusStopCases
}

type (
	TaskFn[Pipe TaskListData]           func(t *Task[Pipe]) error
	TaskPredicateFn[Pipe TaskListData]  func(t *Task[Pipe]) bool
	TaskJobWrapperFn[Pipe TaskListData] func(job Job, t *Task[Pipe]) Job
	TaskJobParserFn[Pipe TaskListData]  func(t *Task[Pipe]) Job
)

// NewTask Creates a new task to be run as a job.
func NewTask[Pipe TaskListData](tl *TaskList[Pipe], name ...string) *Task[Pipe] {
	t := &Task[Pipe]{
		Name:     strings.Join(utils.DeleteEmptyStringsFromSlice(name), tl.Plumber.options.delimiter),
		TL:       tl,
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
	t.options.disablePredicateFn = fn

	return t
}

// Checks whether the current task is disabled or not.
func (t *Task[Pipe]) IsDisabled() bool {
	if t.options.disablePredicateFn == nil {
		return false
	}

	return t.options.disablePredicateFn(t)
}

// Sets the predicate that should conditionally skip the task depending on the pipe variables.
func (t *Task[Pipe]) ShouldSkip(fn TaskPredicateFn[Pipe]) *Task[Pipe] {
	t.options.skipPredicateFn = fn

	return t
}

// Checks whether the current task is skipped or not.
func (t *Task[Pipe]) IsSkipped() bool {
	if t.options.skipPredicateFn == nil {
		return false
	}

	return t.options.skipPredicateFn(t)
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

// Extend the job of the current task.
func (t *Task[Pipe]) SetJobWrapper(fn TaskJobWrapperFn[Pipe]) *Task[Pipe] {
	t.jobWrapperFn = fn

	return t
}

// Runs the current task.
func (t *Task[Pipe]) Run() error {
	if stop := t.handleStopCases(); stop {
		return nil
	}

	started := time.Now()
	t.Log.WithField(LOG_FIELD_STATUS, log_status_run).Traceln(t.Name)

	if t.shouldRunBeforeFn != nil {
		if err := t.shouldRunBeforeFn(t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
	}

	if t.fn != nil {
		if err := t.fn(t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
	}

	if t.shouldRunAfterFn != nil {
		if err := t.shouldRunAfterFn(t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
	}

	t.Log.WithField(LOG_FIELD_STATUS, log_status_end).Tracef("%s -> %s", t.Name, time.Since(started).Round(time.Millisecond).String())

	return nil
}

// Runs the current task as a job.
func (t *Task[Pipe]) Job() Job {
	return t.TL.JobIfNot(
		t.TL.Predicate(func(tl *TaskList[Pipe]) bool {
			return t.handleStopCases()
		}),
		t.TL.CreateJob(func(tl *TaskList[Pipe]) error {
			if t.jobWrapperFn != nil {
				return tl.RunJobs(t.jobWrapperFn(
					tl.CreateBasicJob(t.Run),
					t,
				))
			}

			return t.Run()
		}),
		t.TL.CreateJob(func(tl *TaskList[Pipe]) error {
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
	if t.status.stopCases.handled {
		return t.status.stopCases.result
	}

	t.status.stopCases.handled = true

	if result := t.IsDisabled(); result {
		t.Log.WithField(LOG_FIELD_CONTEXT, log_context_disable).
			Debugf("%s", t.Name)

		t.status.stopCases.result = true
		return t.status.stopCases.result
	} else if result := t.IsSkipped(); result {
		t.Log.WithField(LOG_FIELD_CONTEXT, log_context_skipped).
			Warnf("%s", t.Name)

		t.status.stopCases.result = true
		return t.status.stopCases.result
	}

	t.status.stopCases.result = false
	return t.status.stopCases.result
}

// Handles the errors from the current task.
func (t *Task[Pipe]) handleErrors(err error) error {
	t.SendFatal(err)

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
