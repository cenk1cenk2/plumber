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
	tasks             Job
	shouldRunBeforeFn TaskListFn[Pipe]
	shouldRunAfterFn  TaskListFn[Pipe]
}

type (
	TaskListFn[Pipe TaskListData]          func(*TaskList[Pipe]) error
	TaskListJobFn[Pipe TaskListData]       func(*TaskList[Pipe]) Job
	TaskListPredicateFn[Pipe TaskListData] func(*TaskList[Pipe]) bool
)

type TaskListOptions[Pipe TaskListData] struct {
	Skip         TaskListPredicateFn[Pipe]
	Disable      TaskListPredicateFn[Pipe]
	runtimeDepth int
}

// Creates a new task list and initiates it.
func NewTaskList[Pipe TaskListData](p *Plumber) *TaskList[Pipe] {
	t := &TaskList[Pipe]{}

	return t.New(p)
}

// Creates a new task list.
func (t *TaskList[Pipe]) New(p *Plumber) *TaskList[Pipe] {
	t.Lock = &sync.RWMutex{}
	t.Plumber = p
	t.Channel = &p.Channel
	t.options.runtimeDepth = 3

	t.setupLogger()

	t.flocContext = floc.NewContext()
	t.Control = floc.NewControl(t.flocContext)
	go t.registerTerminateHandler()

	return t
}

// Sets the function that should run before the task list.
func (t *TaskList[Pipe]) ShouldRunBefore(fn TaskListFn[Pipe]) *TaskList[Pipe] {
	t.Lock.Lock()
	t.shouldRunBeforeFn = fn
	t.Lock.Unlock()

	return t
}

// Returns the current tasks inside the task list as job.
func (t *TaskList[Pipe]) GetTasks() Job {
	return t.tasks
}

// Sets the tasks of the task list.
func (t *TaskList[Pipe]) Set(fn TaskListJobFn[Pipe]) *TaskList[Pipe] {
	t.Lock.Lock()
	t.tasks = fn(t)
	t.Lock.Unlock()

	return t
}

// Sets the tasks of the task list with wrapper.
func (t *TaskList[Pipe]) SetTasks(tasks Job) *TaskList[Pipe] {
	t.Lock.Lock()
	t.tasks = tasks
	t.Lock.Unlock()

	return t
}

// Sets the function that should run after the task list.
func (t *TaskList[Pipe]) ShouldRunAfter(fn TaskListFn[Pipe]) *TaskList[Pipe] {
	t.Lock.Lock()
	t.shouldRunAfterFn = fn
	t.Lock.Unlock()

	return t
}

// Sets the predicate that should conditionally disable the task list depending on the pipe variables.
func (t *TaskList[Pipe]) ShouldDisable(fn TaskListPredicateFn[Pipe]) *TaskList[Pipe] {
	t.options.Disable = fn

	return t
}

// Checks whether the current task is disabled or not.
func (t *TaskList[Pipe]) IsDisabled() bool {
	if t.options.Disable == nil {
		return false
	}

	return t.options.Disable(t)
}

// Sets the predicate that should conditionally skip the task list depending on the pipe variables.
func (t *TaskList[Pipe]) ShouldSkip(fn TaskListPredicateFn[Pipe]) *TaskList[Pipe] {
	t.options.Skip = fn

	return t
}

// Checks whether the current task is skipped or not.
func (t *TaskList[Pipe]) IsSkipped() bool {
	if t.options.Skip == nil {
		return false
	}

	return t.options.Skip(t)
}

// Creates a new task.
func (t *TaskList[Pipe]) CreateTask(name ...string) *Task[Pipe] {
	return NewTask(t, name...)
}

// Sets the CLI context for urfave/cli that is coming from the action function.
func (t *TaskList[Pipe]) SetCliContext(ctx *cli.Context) *TaskList[Pipe] {
	t.Lock.Lock()
	t.CliContext = ctx
	t.Lock.Unlock()

	return t
}

// Sets the runtime depth for the logger.
func (t *TaskList[Pipe]) SetRuntimeDepth(depth int) *TaskList[Pipe] {
	t.options.runtimeDepth = depth
	t.setupLogger()

	return t
}

// Validates the current pipe of the task list.
func (t *TaskList[Pipe]) Validate(data TaskListData) error {
	if err := defaults.Set(data); err != nil {
		return fmt.Errorf("Can not set defaults: %w", err)
	}

	validate := validator.New()

	err := validate.Struct(data)

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
	if t.tasks == nil {
		return fmt.Errorf("Task list is empty.")
	}

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

	{
		if err := t.Validate(&t.Pipe); err != nil {
			return err
		}

		result, data, err := floc.RunWith(t.flocContext, t.Control, t.tasks)

		if err != nil {
			return err
		}

		if err := t.handleFloc(result, data); err != nil {
			return err
		}
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
