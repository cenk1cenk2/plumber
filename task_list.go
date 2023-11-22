package plumber

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/workanator/go-floc/v3"

	"fmt"

	"github.com/creasty/defaults"
	validator "github.com/go-playground/validator/v10"
)

type TaskListData interface {
	any
}

type TaskList[Pipe TaskListData] struct {
	Plumber    *Plumber
	CliContext *cli.Context
	Pipe       Pipe
	Channel    *AppChannel
	Control    floc.Control

	Name    string
	options TaskListOptions[Pipe]
	Lock    *sync.RWMutex
	Log     *logrus.Entry

	flocContext       floc.Context
	shouldRunBeforeFn TaskListFn[Pipe]
	fn                TaskListJobFn[Pipe]
	shouldRunAfterFn  TaskListFn[Pipe]
}

type (
	TaskListFn[Pipe TaskListData]          func(tl *TaskList[Pipe]) error
	TaskListJobFn[Pipe TaskListData]       func(tl *TaskList[Pipe]) Job
	TaskListPredicateFn[Pipe TaskListData] func(tl *TaskList[Pipe]) bool
)

type TaskListOptions[Pipe TaskListData] struct {
	skipFn       TaskListPredicateFn[Pipe]
	disableFn    TaskListPredicateFn[Pipe]
	runtimeDepth int
}

// Creates a new task list and initiates it.
func NewTaskList[Pipe TaskListData](p *Plumber) *TaskList[Pipe] {
	t := &TaskList[Pipe]{}

	return t.New(p)
}

// Creates a new task list.
func (tl *TaskList[Pipe]) New(p *Plumber) *TaskList[Pipe] {
	tl.Lock = &sync.RWMutex{}
	tl.Plumber = p
	tl.Channel = &p.Channel

	tl.setupLogger()

	tl.flocContext = floc.NewContext()
	tl.Control = floc.NewControl(tl.flocContext)
	go tl.registerTerminateHandler()

	return tl
}

// Sets the function that should run before the task list.
func (tl *TaskList[Pipe]) ShouldRunBefore(fn TaskListFn[Pipe]) *TaskList[Pipe] {
	tl.Lock.Lock()
	tl.shouldRunBeforeFn = fn
	tl.Lock.Unlock()

	return tl
}

// Sets the tasks of the task list.
func (tl *TaskList[Pipe]) Set(fn TaskListJobFn[Pipe]) *TaskList[Pipe] {
	tl.Lock.Lock()
	tl.fn = fn
	tl.Lock.Unlock()

	return tl
}

// Sets the function that should run after the task list.
func (tl *TaskList[Pipe]) ShouldRunAfter(fn TaskListFn[Pipe]) *TaskList[Pipe] {
	tl.Lock.Lock()
	tl.shouldRunAfterFn = fn
	tl.Lock.Unlock()

	return tl
}

// Sets the predicate that should conditionally disable the task list depending on the pipe variables.
func (tl *TaskList[Pipe]) ShouldDisable(fn TaskListPredicateFn[Pipe]) *TaskList[Pipe] {
	tl.Lock.Lock()
	tl.options.disableFn = fn
	tl.Lock.Unlock()

	return tl
}

// Checks whether the current task is disabled or not.
func (tl *TaskList[Pipe]) IsDisabled() bool {
	if tl.options.disableFn == nil {
		return false
	}

	return tl.options.disableFn(tl)
}

// Sets the predicate that should conditionally skip the task list depending on the pipe variables.
func (tl *TaskList[Pipe]) ShouldSkip(fn TaskListPredicateFn[Pipe]) *TaskList[Pipe] {
	tl.Lock.Lock()
	tl.options.skipFn = fn
	tl.Lock.Unlock()

	return tl
}

// Checks whether the current task is skipped or not.
func (tl *TaskList[Pipe]) IsSkipped() bool {
	if tl.options.skipFn == nil {
		return false
	}

	return tl.options.skipFn(tl)
}

// Creates a new task.
func (tl *TaskList[Pipe]) CreateTask(name ...string) *Task[Pipe] {
	return NewTask(tl, name...)
}

// Sets the CLI context for urfave/cli that is coming from the action function.
func (tl *TaskList[Pipe]) SetCliContext(ctx *cli.Context) *TaskList[Pipe] {
	tl.Lock.Lock()
	tl.CliContext = ctx
	tl.Lock.Unlock()

	return tl
}

// Sets the runtime depth for the logger.
func (tl *TaskList[Pipe]) SetRuntimeDepth(depth int) *TaskList[Pipe] {
	tl.options.runtimeDepth = depth
	tl.setupLogger()

	return tl
}

// Validates the current pipe of the task list.
func (t *TaskList[Pipe]) Validate(data TaskListData) error {
	if err := defaults.Set(data); err != nil {
		return fmt.Errorf("Can not set defaults: %w", err)
	}

	err := t.Plumber.Validator.Struct(data)

	if err != nil {
		//nolint:errorlint
		for _, err := range err.(validator.ValidationErrors) {
			e := fmt.Sprintf(
				`"%s" field failed validation: %s`,
				err.Namespace(),
				err.Tag(),
			)

			param := err.Param()
			if param != "" {
				e = fmt.Sprintf("%s > %s", e, param)
			}

			t.Log.Errorln(e)
		}

		return fmt.Errorf("Validation failed.")
	}

	return nil
}

// Runs a the provided job.
func (t *TaskList[Pipe]) RunJobs(job Job) error {
	if job == nil {
		return nil
	}

	result, data, err := floc.RunWith(t.flocContext, t.Control, job)

	if err != nil {
		return err
	}

	return t.handleFloc(result, data)
}

// Runs the current task list.
func (t *TaskList[Pipe]) Run() error {
	if stop := t.handleStopCases(); stop {
		return nil
	}

	started := time.Now()

	t.Log.WithField(LOG_FIELD_STATUS, log_status_run).Traceln(t.Name)

	if t.shouldRunBeforeFn != nil {
		if err := t.shouldRunBeforeFn(t); err != nil {
			return err
		}
	}

	if err := t.Validate(&t.Pipe); err != nil {
		return err
	}

	result, data, err := floc.RunWith(t.flocContext, t.Control, t.fn(t))

	if err != nil {
		return err
	}

	if err := t.handleFloc(result, data); err != nil {
		return err
	}

	if t.shouldRunAfterFn != nil {
		if err := t.shouldRunAfterFn(t); err != nil {
			return err
		}
	}

	t.Log.WithField(LOG_FIELD_STATUS, log_status_end).
		Tracef("%s -> %s", t.Name, time.Since(started).Round(time.Millisecond).String())

	return nil
}

// Returns this task list as a job.
func (t *TaskList[Pipe]) Job() Job {
	return func(fctx floc.Context, ctrl floc.Control) error {
		t.flocContext = fctx
		t.Control = ctrl

		return t.Run()
	}
}

// Handles the cases where the task list should not be executed.
func (t *TaskList[Pipe]) handleStopCases() bool {
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

// Handles output coming from floc.
func (t *TaskList[Pipe]) handleFloc(result floc.Result, data interface{}) error {
	switch {
	case result.IsCanceled() && data != nil:
		t.Log.Debugf("Tasks are cancelled: %s", data)
	}

	return nil
}

// Registers the termitor to the current task list.
func (t *TaskList[Pipe]) registerTerminateHandler() {
	if t.Plumber.Enabled {
		ch := make(chan os.Signal, 1)

		t.Plumber.Terminator.ShouldTerminate.Register(ch)
		defer t.Plumber.Terminator.ShouldTerminate.Unregister(ch)

		<-ch

		t.Control.Cancel(fmt.Errorf("Trying to terminate..."))
	}
}

// Sets up logger depending on the depth of the code.
func (t *TaskList[Pipe]) setupLogger() {
	_, file, _, ok := runtime.Caller(2)

	if ok {
		f := strings.Split(file, "/")

		t.Name = strings.Join(f[len(f)-t.options.runtimeDepth:], "/")

		t.Log = t.Plumber.Log.WithField(LOG_FIELD_CONTEXT, t.Name)
	} else {
		t.Log = t.Plumber.Log.WithField(LOG_FIELD_CONTEXT, "TL")
		t.Log.Tracef("Runtime caller has failed using default: %s", file)
	}
}
