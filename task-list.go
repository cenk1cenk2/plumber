package plumber

import (
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

	flocContext floc.Context
	runBefore   taskListFn[Pipe]
	runAfter    taskListFn[Pipe]
}

type (
	taskListFn[Pipe TaskListData] func(*TaskList[Pipe]) error
)

func (t *TaskList[Pipe]) New(p *Plumber) *TaskList[Pipe] {
	t.Lock = &sync.RWMutex{}
	t.Plumber = p
	t.Log = p.Log
	t.Channel = &p.Channel

	t.flocContext = floc.NewContext()
	t.Control = floc.NewControl(t.flocContext)

	return t
}

func (t *TaskList[Pipe]) SetCliContext(ctx *cli.Context) *TaskList[Pipe] {
	t.Lock.Lock()
	t.CliContext = ctx
	t.Lock.Unlock()

	return t
}

func (t *TaskList[Pipe]) GetTasks() Job {
	return t.Tasks
}

func (t *TaskList[Pipe]) SetTasks(tasks Job) *TaskList[Pipe] {
	t.Lock.Lock()
	t.Tasks = tasks
	t.Lock.Unlock()

	return t
}

func (t *TaskList[Pipe]) CreateTask(name string) *Task[Pipe] {
	return NewTask(t, name)
}

func (t *TaskList[Pipe]) ShouldRunBefore(fn taskListFn[Pipe]) *TaskList[Pipe] {
	t.Lock.Lock()
	t.runBefore = fn
	t.Lock.Unlock()

	return t
}

func (t *TaskList[Pipe]) ShouldRunAfter(fn taskListFn[Pipe]) *TaskList[Pipe] {
	t.Lock.Lock()
	t.runAfter = fn
	t.Lock.Unlock()

	return t
}

func (t *TaskList[Pipe]) Validate(data TaskListData) error {
	if err := defaults.Set(data); err != nil {
		return fmt.Errorf("Can not set defaults: %s", err)
	}

	validate := validator.New()

	err := validate.Struct(data)

	if err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			error := fmt.Sprintf(
				`"%s" field failed validation: %s`,
				err.Namespace(),
				err.Tag(),
			)

			param := err.Param()
			if param != "" {
				error = fmt.Sprintf("%s > %s", error, param)
			}

			t.Log.Errorln(error)
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

	result, data, err := floc.RunWith(t.flocContext, t.Control, floc.Job(t.Tasks))

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

	result, data, err := floc.RunWith(t.flocContext, t.Control, floc.Job(job))

	if err != nil {
		return err
	}

	if err := t.handleFloc(result, data); err != nil {
		return err
	}

	return nil
}

func (t *TaskList[Pipe]) handleFloc(result floc.Result, data interface{}) error {
	switch {
	case result.IsCanceled() && data != nil:
		return fmt.Errorf("Tasks are cancelled: %s", data.(string))
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
