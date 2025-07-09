package plumber

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
	"gitlab.kilic.dev/libraries/go-utils/v2/utils"
)

type Task struct {
	Plumber *Plumber
	TL      *TaskList
	Log     *logrus.Entry
	Channel *AppChannel
	Name    string

	Lock     *sync.RWMutex
	taskLock *sync.RWMutex

	options           TaskOptions
	commands          []*Command
	parent            *Task
	subtask           Job
	emptyJob          Job
	fn                TaskFn
	shouldRunBeforeFn TaskFn
	shouldRunAfterFn  TaskFn
	onTerminatorFn    TaskFn
	jobWrapperFn      TaskJobWrapperFn
	status            TaskStatus
}

type TaskOptions struct {
	skipPredicateFn    TaskPredicateFn
	disablePredicateFn TaskPredicateFn
}

type TaskStatus struct {
	stopCases StatusStopCases
}

type (
	TaskFn           func(t *Task) error
	TaskPredicateFn  func(t *Task) bool
	TaskJobWrapperFn func(job Job, t *Task) Job
	TaskJobParserFn  func(t *Task) Job
)

// NewTask Creates a new task to be run as a job.
func NewTask(tl *TaskList, name ...string) *Task {
	t := &Task{
		Name:     strings.Join(utils.DeleteEmptyStringsFromSlice(name), tl.Plumber.options.delimiter),
		TL:       tl,
		Plumber:  tl.Plumber,
		Lock:     tl.Lock,
		taskLock: &sync.RWMutex{},
		Channel:  tl.Channel,
	}

	t.Log = tl.Log.WithField(LOG_FIELD_CONTEXT, t.Name)

	t.emptyJob = JobIf(Predicate(func() bool {
		return false
	}),
		func(_ floc.Context, _ floc.Control) error {
			return nil
		},
	)
	t.subtask = t.emptyJob

	return t
}

// Sets the function that should run before the task.
func (t *Task) ShouldRunBefore(fn TaskFn) *Task {
	t.shouldRunBeforeFn = fn

	return t
}

// Sets the function that should run as task.
func (t *Task) Set(fn TaskFn) *Task {
	t.fn = fn

	return t
}

// Sets the function that should run after the task.
func (t *Task) ShouldRunAfter(fn TaskFn) *Task {
	t.shouldRunAfterFn = fn

	return t
}

// Sets the predicate that should conditionally disable the task depending on the pipe variables.
func (t *Task) ShouldDisable(fn TaskPredicateFn) *Task {
	t.options.disablePredicateFn = fn

	return t
}

// Checks whether the current task is disabled or not.
func (t *Task) IsDisabled() bool {
	if t.options.disablePredicateFn == nil {
		return false
	}

	return t.options.disablePredicateFn(t)
}

// Sets the predicate that should conditionally skip the task depending on the pipe variables.
func (t *Task) ShouldSkip(fn TaskPredicateFn) *Task {
	t.options.skipPredicateFn = fn

	return t
}

// Checks whether the current task is skipped or not.
func (t *Task) IsSkipped() bool {
	if t.options.skipPredicateFn == nil {
		return false
	}

	return t.options.skipPredicateFn(t)
}

// Enables global plumber terminator on this task.
func (t *Task) EnableTerminator() *Task {
	t.Log.Tracef("Registered terminator.")
	t.Plumber.RegisterTerminator()

	go t.handleTerminator()

	return t
}

// Sets the function that should fire whenever the application is globally terminated.
func (t *Task) SetOnTerminator(fn TaskFn) *Task {
	t.onTerminatorFn = fn

	return t
}

// Extend the job of the current task.
func (t *Task) SetJobWrapper(fn TaskJobWrapperFn) *Task {
	t.jobWrapperFn = fn

	return t
}

// Runs the current task.
func (t *Task) Run() error {
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
func (t *Task) Job() Job {
	return JobIfNot(
		Predicate(func() bool {
			return t.handleStopCases()
		}),
		CreateJob(func() error {
			if t.jobWrapperFn != nil {
				return t.Plumber.RunJobs(t.jobWrapperFn(
					CreateBasicJob(t.Run),
					t,
				))
			}

			return t.Run()
		}),
		CreateJob(func() error {
			return nil
		}),
	)
}

// Send the error message to plumber while running inside a routine.
func (t *Task) SendError(err error) *Task {
	t.Plumber.SendError(t.Log, err)

	return t
}

// Send the fatal error message to plumber while running inside a routine.
func (t *Task) SendFatal(err error) *Task {
	t.Plumber.SendFatal(t.Log, err)

	return t
}

// Trigger the exit protocol of plumber.
func (t *Task) SendExit(code int) *Task {
	t.Plumber.SendExit(code)

	return t
}

// Handles the stop cases of the task.
func (t *Task) handleStopCases() bool {
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
func (t *Task) handleErrors(err error) error {
	t.SendFatal(err)

	return err
}

// Handles the plumber terminator when terminator is triggered.
func (t *Task) handleTerminator() {
	if t.IsDisabled() || t.IsSkipped() {
		t.Log.Traceln("Sending terminated directly because the task is already not available.")

		t.Plumber.DeregisterTerminator()

		return
	}

	ch := make(chan os.Signal, 1)

	t.Plumber.Terminator.ShouldTerminate.Register(ch)

	sig := <-ch

	t.Log.Tracef("Forwarding signal to task: %s", sig)

	if t.onTerminatorFn != nil {
		t.SendError(t.onTerminatorFn(t))
	}

	t.Log.Tracef("Registered as terminated.")
	t.Plumber.RegisterTerminated()
}
