package plumber

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type TaskList struct {
	Plumber *Plumber

	Name    string
	options TaskListOptions
	Lock    *sync.RWMutex
	Log     *logrus.Entry

	shouldRunBeforeFn TaskListFn
	fn                TaskListJobFn
	shouldRunAfterFn  TaskListFn
	runtime           Runtime
}

type (
	TaskListFn          func(ctx context.Context, tl *TaskList) error
	TaskListJobFn       func(tl *TaskList) Job
	TaskListPredicateFn func(tl *TaskList) bool
)

type TaskListOptions struct {
	skipFn       TaskListPredicateFn
	disableFn    TaskListPredicateFn
	runtimeDepth int
}

// Creates a new task list and initiates it.
func NewTaskList(p *Plumber) *TaskList {
	t := &TaskList{}

	return t.New(p)
}

// Creates a new task list.
func (tl *TaskList) New(p *Plumber) *TaskList {
	tl.Lock = &sync.RWMutex{}
	tl.Plumber = p
	tl.options.runtimeDepth = 1

	tl.setupLogger()

	return tl
}

// Sets the function that should run before the task list.
func (p *TaskList) ShouldRunBefore(fn TaskListFn) *TaskList {
	p.Lock.Lock()
	p.shouldRunBeforeFn = fn
	p.Lock.Unlock()

	return p
}

// Sets the tasks of the task list.
func (p *TaskList) Set(fn TaskListJobFn) *TaskList {
	p.Lock.Lock()
	p.fn = fn
	p.Lock.Unlock()

	return p
}

// Sets the function that should run after the task list.
func (p *TaskList) ShouldRunAfter(fn TaskListFn) *TaskList {
	p.Lock.Lock()
	p.shouldRunAfterFn = fn
	p.Lock.Unlock()

	return p
}

func (p *TaskList) SetRuntime(runtime Runtime) *TaskList {
	p.Lock.Lock()
	p.runtime = runtime
	p.Lock.Unlock()

	return p
}

// Sets the predicate that should conditionally disable the task list depending on the pipe variables.
func (p *TaskList) ShouldDisable(fn TaskListPredicateFn) *TaskList {
	p.Lock.Lock()
	p.options.disableFn = fn
	p.Lock.Unlock()

	return p
}

// Checks whether the current task is disabled or not.
func (p *TaskList) IsDisabled() bool {
	if p.options.disableFn == nil {
		return false
	}

	return p.options.disableFn(p)
}

// Sets the predicate that should conditionally skip the task list depending on the pipe variables.
func (p *TaskList) ShouldSkip(fn TaskListPredicateFn) *TaskList {
	p.Lock.Lock()
	p.options.skipFn = fn
	p.Lock.Unlock()

	return p
}

// Checks whether the current task is skipped or not.
func (p *TaskList) IsSkipped() bool {
	if p.options.skipFn == nil {
		return false
	}

	return p.options.skipFn(p)
}

// Creates a new task.
func (p *TaskList) CreateTask(name ...string) *Task {
	return NewTask(p, name...)
}

// Sets the runtime depth for the logger.
func (p *TaskList) SetRuntimeDepth(depth int) *TaskList {
	p.options.runtimeDepth = depth
	p.setupLogger()

	return p
}

func (p *TaskList) RunBefore(ctx context.Context) error {
	if stop := p.handleStopCases(); stop {
		return nil
	}

	started := time.Now()

	p.Log.WithField(LOG_FIELD_STATUS, log_status_run).Tracef("ShouldRunBefore: %s", p.Name)

	if p.shouldRunBeforeFn != nil {
		if err := p.shouldRunBeforeFn(ctx, p); err != nil {
			return err
		}
	}

	p.Log.WithField(LOG_FIELD_STATUS, log_status_end).
		Tracef("ShouldRunBefore: %s -> %s", p.Name, time.Since(started).Round(time.Millisecond).String())

	return nil
}

// Runs the current task list.
func (p *TaskList) Run(ctx context.Context) error {
	if stop := p.handleStopCases(); stop {
		return nil
	}

	started := time.Now()

	p.Log.WithField(LOG_FIELD_STATUS, log_status_run).Tracef("Run: %s", p.Name)

	if err := p.Plumber.runJobs(ctx, p.fn(p)); err != nil {
		return err
	}

	p.Log.WithField(LOG_FIELD_STATUS, log_status_end).
		Tracef("Run: %s -> %s", p.Name, time.Since(started).Round(time.Millisecond).String())

	return nil
}

func (p *TaskList) RunWith(ctx context.Context, runtime Runtime) error {
	scoped := *p
	scoped.Lock = &sync.RWMutex{}
	scoped.runtime = runtime.inherit(p.runtime)

	return scoped.Run(ctx)
}

func (p *TaskList) RunAfter(ctx context.Context) error {
	if stop := p.handleStopCases(); stop {
		return nil
	}

	started := time.Now()

	p.Log.WithField(LOG_FIELD_STATUS, log_status_run).Tracef("ShouldRunAfter: %s", p.Name)

	if p.shouldRunAfterFn != nil {
		if err := p.shouldRunAfterFn(ctx, p); err != nil {
			return err
		}
	}

	p.Log.WithField(LOG_FIELD_STATUS, log_status_end).
		Tracef("ShouldRunAfter: %s -> %s", p.Name, time.Since(started).Round(time.Millisecond).String())

	return nil
}

func (p *TaskList) JobBefore() Job {
	return func(ctx context.Context) error {
		return p.RunBefore(ctx)
	}
}

// Returns this task list as a job.
func (p *TaskList) Job() Job {
	return func(ctx context.Context) error {
		return p.Run(ctx)
	}
}

func (p *TaskList) JobAfter() Job {
	return func(ctx context.Context) error {
		return p.RunAfter(ctx)
	}
}

// Handles the cases where the task list should not be executed.
func (p *TaskList) handleStopCases() bool {
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

// Sets up logger depending on the depth of the code.
func (p *TaskList) setupLogger() {
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

func CombineTaskLists(tls ...TaskLister) Job {
	before := []Job{}
	job := []Job{}
	after := []Job{}

	for _, tl := range tls {
		before = append(before, GuardIgnoreCancel(tl.JobBefore()))
		job = append(job, tl.Job())
		after = append(after, GuardIgnoreCancel(tl.JobAfter()))
	}

	return JobSequence(
		JobParallel(before...),
		JobSequence(job...),
		JobParallel(after...),
	)
}
