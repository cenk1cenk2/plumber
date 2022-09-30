package plumber

import (
	"os"
	"sync"

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
	Tasks Job

	Plumber *Plumber

	CliContext *cli.Context
	Pipe       Pipe
	Lock       *sync.RWMutex
	Log        *logrus.Logger
	Channel    *AppChannel
	Control    floc.Control

	delimiter   string
	flocContext floc.Context
	runBefore   TaskListFn[Pipe]
	runAfter    TaskListFn[Pipe]
}

type (
	TaskListFn[Pipe TaskListData]    func(*TaskList[Pipe]) error
	TaskListJobFn[Pipe TaskListData] func(*TaskList[Pipe]) Job
)

func (t *TaskList[Pipe]) New(p *Plumber) *TaskList[Pipe] {
	t.Lock = &sync.RWMutex{}
	t.Plumber = p
	t.Log = p.Log
	t.Channel = &p.Channel
	t.delimiter = ":"

	t.flocContext = floc.NewContext()
	t.Control = floc.NewControl(t.flocContext)
	go t.registerTerminateHandler()

	return t
}

func (t *TaskList[Pipe]) SetCliContext(ctx *cli.Context) *TaskList[Pipe] {
	t.Lock.Lock()
	t.CliContext = ctx
	t.Lock.Unlock()

	return t
}

func (t *TaskList[Pipe]) SetDelimiter(delimiter string) *TaskList[Pipe] {
	t.delimiter = delimiter

	return t
}

func (t *TaskList[Pipe]) GetTasks() Job {
	return t.Tasks
}

func (t *TaskList[Pipe]) Set(fn TaskListJobFn[Pipe]) *TaskList[Pipe] {
	t.Lock.Lock()
	t.Tasks = fn(t)
	t.Lock.Unlock()

	return t
}

func (t *TaskList[Pipe]) SetTasks(tasks Job) *TaskList[Pipe] {
	t.Lock.Lock()
	t.Tasks = tasks
	t.Lock.Unlock()

	return t
}

func (t *TaskList[Pipe]) CreateTask(name ...string) *Task[Pipe] {
	return NewTask(t, name...)
}

func (t *TaskList[Pipe]) ShouldRunBefore(fn TaskListFn[Pipe]) *TaskList[Pipe] {
	t.Lock.Lock()
	t.runBefore = fn
	t.Lock.Unlock()

	return t
}

func (t *TaskList[Pipe]) ShouldRunAfter(fn TaskListFn[Pipe]) *TaskList[Pipe] {
	t.Lock.Lock()
	t.runAfter = fn
	t.Lock.Unlock()

	return t
}

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

func (t *TaskList[Pipe]) Run() error {
	if err := t.Validate(&t.Pipe); err != nil {
		return err
	}

	if t.runBefore != nil {
		if err := t.runBefore(t); err != nil {
			return err
		}
	}

	if t.Tasks == nil {
		return fmt.Errorf("Task list is empty.")
	}

	result, data, err := floc.RunWith(t.flocContext, t.Control, t.Tasks)

	if err != nil {
		return err
	}

	if err := t.handleFloc(result, data); err != nil {
		return err
	}

	if t.runAfter != nil {
		if err := t.runAfter(t); err != nil {
			return err
		}
	}

	return nil
}

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

func (t *TaskList[Pipe]) handleFloc(result floc.Result, data interface{}) error {
	switch {
	case result.IsCanceled() && data != nil:
		t.Log.Debugf("Tasks are cancelled: %s", data)
	}

	return nil
}

func (t *TaskList[Pipe]) Job() Job {
	return func(fctx floc.Context, ctrl floc.Control) error {
		t.flocContext = fctx
		t.Control = ctrl

		return t.Run()
	}
}

func (t *TaskList[Pipe]) registerTerminateHandler() {
	ch := make(chan os.Signal, 1)

	t.Plumber.Terminator.ShouldTerminate.Register(ch)

	<-ch

	// t.Control.Cancel(fmt.Errorf("Trying to terminate..."))
}
