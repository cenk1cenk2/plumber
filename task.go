package plumber

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

type Task struct {
	Plumber *Plumber
	TL      *TaskList
	Log     *Logger
	Name    string

	Lock     *sync.RWMutex
	taskLock *sync.RWMutex

	options           TaskOptions
	commands          []*Command
	parent            *Task
	subtask           Job
	fn                TaskFn
	shouldRunBeforeFn TaskFn
	shouldRunAfterFn  TaskFn
	onTerminatorFn    TaskFn
	jobWrapperFn      TaskJobWrapperFn
	runtime           Runtime
	status            TaskStatus
}

type TaskOptions struct {
	skipPredicateFn    TaskPredicateFn
	disablePredicateFn TaskPredicateFn
	terminator         bool
}

type TaskStatus struct {
	stopCases StatusStopCases
}

type (
	TaskFn           func(ctx context.Context, t *Task) error
	TaskPredicateFn  func(t *Task) bool
	TaskJobWrapperFn func(job Job, t *Task) Job
	TaskJobParserFn  func(t *Task) Job
)

// NewTask Creates a new task to be run as a job.
func NewTask(tl *TaskList, name ...string) *Task {
	t := &Task{
		Name: strings.Join(
			slices.DeleteFunc(name, func(v string) bool {
				return v == ""
			}),
			tl.Plumber.options.delimiter,
		),
		TL:       tl,
		Plumber:  tl.Plumber,
		Lock:     tl.Lock,
		taskLock: &sync.RWMutex{},
	}

	t.Log = tl.Log.With(LOG_FIELD_CONTEXT, t.Name)

	t.subtask = CreateEmptyJob()

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

// Enables global plumber terminator on this task, which registers the task to the terminator for as
// long as it is running.
func (t *Task) EnableTerminator() *Task {
	if !t.Plumber.ensureTerminator() {
		return t
	}

	t.Log.Tracef("Enabled terminator.")

	t.options.terminator = true

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

func (t *Task) SetRuntime(runtime Runtime) *Task {
	t.runtime = runtime

	return t
}

// Runs the current task.
func (t *Task) Run(ctx context.Context) error {
	if stop := t.handleStopCases(); stop {
		return nil
	}

	if t.options.terminator {
		release := t.Plumber.registerTerminatorHook(t.handleTerminator)
		defer release()
	}

	started := time.Now()
	t.Log.With(LOG_FIELD_STATUS, log_status_run).Traceln(t.Name)

	if t.shouldRunBeforeFn != nil {
		if err := t.shouldRunBeforeFn(ctx, t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
	}

	if t.fn != nil {
		if err := t.fn(ctx, t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
	}

	if t.shouldRunAfterFn != nil {
		if err := t.shouldRunAfterFn(ctx, t); err != nil {
			t.Log.Errorln(err)

			return t.handleErrors(err)
		}
	}

	t.Log.With(LOG_FIELD_STATUS, log_status_end).Tracef("%s -> %s", t.Name, time.Since(started).Round(time.Millisecond).String())

	return nil
}

func (t *Task) RunWith(ctx context.Context, runtime Runtime) error {
	scoped := *t
	scoped.runtime = runtime.inherit(t.runtime)
	scoped.taskLock = &sync.RWMutex{}
	scoped.commands = make([]*Command, 0, len(t.commands))
	for _, command := range t.commands {
		scopedCommand := *command
		scopedCommand.T = &scoped
		scopedCommand.TL = scoped.TL
		scopedCommand.Plumber = scoped.Plumber
		scopedCommand.Log = scoped.Log
		scoped.commands = append(scoped.commands, &scopedCommand)
	}

	return scoped.Run(ctx)
}

// Runs the current task as a job.
func (t *Task) Job() Job {
	return JobIfNot(
		Predicate(func() bool {
			return t.handleStopCases()
		}),
		func(ctx context.Context) error {
			run := func(ctx context.Context) error {
				return t.Run(ctx)
			}

			if t.jobWrapperFn != nil {
				return t.jobWrapperFn(run, t)(ctx)
			}

			return run(ctx)
		},
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
		t.Log.With(LOG_FIELD_CONTEXT, log_context_disable).
			Debugf("%s", t.Name)

		t.status.stopCases.result = true
		return t.status.stopCases.result
	} else if result := t.IsSkipped(); result {
		t.Log.With(LOG_FIELD_CONTEXT, log_context_skipped).
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

// Handles the plumber terminator when terminator is triggered while the task is running.
func (t *Task) handleTerminator(ctx context.Context) {
	if t.onTerminatorFn == nil {
		return
	}

	t.Log.Traceln("Forwarding terminator to the task.")

	t.SendError(t.onTerminatorFn(ctx, t))

	t.Log.Traceln("Registered as terminated.")
}
