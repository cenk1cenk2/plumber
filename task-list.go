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
	Tasks floc.Job

	App *App

	Pipe    Pipe
	Lock    *sync.RWMutex
	Log     *logrus.Logger
	Channel *AppChannel
	Control floc.Control

	flocContext floc.Context
	runBefore   taskListFn[Pipe]
	runAfter    taskListFn[Pipe]
}

type (
	taskListFn[Pipe TaskListData] func(*TaskList[Pipe], *cli.Context) error
)

func (t *TaskList[Pipe]) New(a *App) *TaskList[Pipe] {
	t.App = a
	t.Log = a.Log
	t.Channel = &a.Channel
	t.Lock = &sync.RWMutex{}

	t.runBefore = func(tl *TaskList[Pipe], ctx *cli.Context) error {
		return nil
	}
	t.runAfter = func(tl *TaskList[Pipe], ctx *cli.Context) error {
		return nil
	}

	t.flocContext = floc.NewContext()
	t.Control = floc.NewControl(t.flocContext)

	return t
}

func (t *TaskList[Pipe]) GetTasks() floc.Job {
	return t.Tasks
}

func (t *TaskList[Pipe]) SetTasks(tasks floc.Job) *TaskList[Pipe] {
	t.Tasks = tasks

	return t
}

func (t *TaskList[Pipe]) CreateTask(name string) *Task[Pipe] {
	task := &Task[Pipe]{}

	return task.New(t, name)
}

func (t *TaskList[Pipe]) ShouldRunBefore(fn taskListFn[Pipe]) *TaskList[Pipe] {
	t.runBefore = fn

	return t
}

func (t *TaskList[Pipe]) ShouldRunAfter(fn taskListFn[Pipe]) *TaskList[Pipe] {
	t.runAfter = fn

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
				error = fmt.Sprintf("%s -> %s", error, param)
			}

			t.Log.Errorln(error)
		}

		return fmt.Errorf("Validation failed.")
	}

	return nil
}

func (t *TaskList[Pipe]) Run(c *cli.Context) error {
	if err := t.Validate(&t.Pipe); err != nil {
		return err
	}

	if err := t.runBefore(t, c); err != nil {
		return err
	}

	if t.Tasks == nil {
		return fmt.Errorf("Task list is empty.")
	}

	result, data, err := floc.RunWith(t.flocContext, t.Control, t.Tasks)
	if err != nil {
		return err
	}

	switch {
	case result.IsCanceled():
		return fmt.Errorf(data.(string))
	case result.IsFailed():
		return fmt.Errorf(data.(string))
	}

	if err := t.runAfter(t, c); err != nil {
		return err
	}

	return nil
}

func (t *TaskList[Pipe]) RunJobs(job floc.Job) error {
	if job == nil {
		return nil
	}

	result, data, err := floc.RunWith(t.flocContext, t.Control, job)

	if err != nil {
		return err
	}

	switch {
	case result.IsCanceled():
		return fmt.Errorf(data.(string))
	case result.IsFailed():
		return fmt.Errorf(data.(string))
	}

	return nil
}

func (t *TaskList[Pipe]) Job(c *cli.Context) floc.Job {
	return func(ctx floc.Context, ctrl floc.Control) error {
		t.flocContext = ctx
		t.Control = ctrl

		return t.Run(c)
	}
}
