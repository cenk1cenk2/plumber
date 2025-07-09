package plumber

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	"github.com/workanator/go-floc/v3"

	"fmt"

	"github.com/creasty/defaults"
	validator "github.com/go-playground/validator/v10"
)

type TaskListData interface {
	any
}

type TaskList[Pipe TaskListData] struct {
	Plumber *Plumber
	Cli     *cli.Command
	Pipe    Pipe
	Channel *AppChannel

	Name    string
	options TaskListOptions[Pipe]
	Lock    *sync.RWMutex
	Log     *logrus.Entry

	shouldRunBeforeFn TaskListFn[Pipe]
	fn                TaskListJobFn[Pipe]
	shouldRunAfterFn  TaskListFn[Pipe]
}

type TaskListCtx struct {
	Plumber *Plumber
	Cli     *cli.Command

	Name string
	Lock *sync.RWMutex
	Log  *logrus.Entry
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
	tl.options.runtimeDepth = 1

	tl.setupLogger()

	go tl.registerTerminateHandler()

	return tl
}

// Sets the function that should run before the task list.
func (p *TaskList[Pipe]) ShouldRunBefore(fn TaskListFn[Pipe]) *TaskList[Pipe] {
	p.Lock.Lock()
	p.shouldRunBeforeFn = fn
	p.Lock.Unlock()

	return p
}

// Sets the tasks of the task list.
func (p *TaskList[Pipe]) Set(fn TaskListJobFn[Pipe]) *TaskList[Pipe] {
	p.Lock.Lock()
	p.fn = fn
	p.Lock.Unlock()

	return p
}

// Sets the function that should run after the task list.
func (p *TaskList[Pipe]) ShouldRunAfter(fn TaskListFn[Pipe]) *TaskList[Pipe] {
	p.Lock.Lock()
	p.shouldRunAfterFn = fn
	p.Lock.Unlock()

	return p
}

// Sets the predicate that should conditionally disable the task list depending on the pipe variables.
func (p *TaskList[Pipe]) ShouldDisable(fn TaskListPredicateFn[Pipe]) *TaskList[Pipe] {
	p.Lock.Lock()
	p.options.disableFn = fn
	p.Lock.Unlock()

	return p
}

// Checks whether the current task is disabled or not.
func (p *TaskList[Pipe]) IsDisabled() bool {
	if p.options.disableFn == nil {
		return false
	}

	return p.options.disableFn(p)
}

// Sets the predicate that should conditionally skip the task list depending on the pipe variables.
func (p *TaskList[Pipe]) ShouldSkip(fn TaskListPredicateFn[Pipe]) *TaskList[Pipe] {
	p.Lock.Lock()
	p.options.skipFn = fn
	p.Lock.Unlock()

	return p
}

// Checks whether the current task is skipped or not.
func (p *TaskList[Pipe]) IsSkipped() bool {
	if p.options.skipFn == nil {
		return false
	}

	return p.options.skipFn(p)
}

// Creates a new task.
func (p *TaskList[Pipe]) CreateTask(name ...string) *Task[Pipe] {
	return NewTask(p, name...)
}

// Sets the CLI context for urfave/cli that is coming from the action function.
func (p *TaskList[Pipe]) SetCli(command *cli.Command) *TaskList[Pipe] {
	p.Lock.Lock()
	p.Cli = command
	p.Lock.Unlock()

	return p
}

// Sets the runtime depth for the logger.
func (p *TaskList[Pipe]) SetRuntimeDepth(depth int) *TaskList[Pipe] {
	p.options.runtimeDepth = depth
	p.setupLogger()

	return p
}

// Validates the current pipe of the task list.
func (p *TaskList[Pipe]) Validate(data TaskListData) error {
	if err := defaults.Set(data); err != nil {
		return fmt.Errorf("Can not set defaults: %w", err)
	}

	err := p.Plumber.Validator.Struct(data)

	if err != nil {
		//nolint:errcheck, errorlint
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

			p.Log.Errorln(e)
		}

		return fmt.Errorf("Validation failed.")
	}

	return nil
}

// Runs the current task list.
func (p *TaskList[Pipe]) Run() error {
	if stop := p.handleStopCases(); stop {
		return nil
	}

	started := time.Now()

	p.Log.WithField(LOG_FIELD_STATUS, log_status_run).Traceln(p.Name)

	if p.shouldRunBeforeFn != nil {
		if err := p.shouldRunBeforeFn(p); err != nil {
			return err
		}
	}

	if err := p.Validate(&p.Pipe); err != nil {
		return err
	}

	result, data, err := floc.RunWith(p.Plumber.flocContext, p.Plumber.flocControl, p.fn(p))

	if err != nil {
		return err
	}

	if err := p.Plumber.handleFloc(result, data); err != nil {
		return err
	}

	if p.shouldRunAfterFn != nil {
		if err := p.shouldRunAfterFn(p); err != nil {
			return err
		}
	}

	p.Log.WithField(LOG_FIELD_STATUS, log_status_end).
		Tracef("%s -> %s", p.Name, time.Since(started).Round(time.Millisecond).String())

	return nil
}

// Returns this task list as a job.
func (p *TaskList[Pipe]) Job() Job {
	return func(_ floc.Context, _ floc.Control) error {
		return p.Run()
	}
}

// Returns the context of the current task list.
func (p *TaskList[Pipe]) ToCtx() *TaskListCtx {
	return &TaskListCtx{
		Plumber: p.Plumber,
		Cli:     p.Cli,
		Name:    p.Name,
		Lock:    p.Lock,
		Log:     p.Log,
	}
}

// Handles the cases where the task list should not be executed.
func (p *TaskList[Pipe]) handleStopCases() bool {
	if result := p.IsDisabled(); result {
		p.Log.WithField(LOG_FIELD_CONTEXT, log_context_disable).
			Debugf("%s", p.Name)

		return true
	} else if result := p.IsSkipped(); result {
		p.Log.WithField(LOG_FIELD_CONTEXT, log_context_skipped).
			Warnf("%s", p.Name)

		return true
	}

	return false
}

// Registers the termitor to the current task list.
func (p *TaskList[Pipe]) registerTerminateHandler() {
	if p.Plumber.Enabled {
		ch := make(chan os.Signal, 1)

		p.Plumber.Terminator.ShouldTerminate.Register(ch)
		defer p.Plumber.Terminator.ShouldTerminate.Unregister(ch)

		<-ch

		p.Plumber.flocControl.Cancel(fmt.Errorf("Trying to terminate..."))
	}
}

// Sets up logger depending on the depth of the code.
func (p *TaskList[Pipe]) setupLogger() {
	_, file, _, ok := runtime.Caller(2)

	if ok {
		f := strings.Split(file, "/")

		p.Name = strings.Join(f[len(f)-p.options.runtimeDepth:], "/")

		p.Log = p.Plumber.Log.WithField(LOG_FIELD_CONTEXT, p.Name)
	} else {
		p.Log = p.Plumber.Log.WithField(LOG_FIELD_CONTEXT, "TL")
		p.Log.Tracef("Runtime caller has failed using default: %s", file)
	}
}
