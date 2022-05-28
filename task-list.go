package plumber

import (
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/workanator/go-floc/v3"

	"errors"
	"fmt"

	"github.com/creasty/defaults"
	validator "github.com/go-playground/validator/v10"
)

type TaskListData interface {
	any
}

type TaskList[Pipe TaskListData, Ctx TaskListData] struct {
	Tasks floc.Job

	App *App

	Pipe     *Pipe
	Context  *Ctx
	Lock     *sync.RWMutex
	Log      *logrus.Logger
	Channnel *AppChannel
	Control  floc.Control

	flocContext floc.Context
	runBefore   taskListFn[Pipe, Ctx]
	runAfter    taskListFn[Pipe, Ctx]
}

type (
	taskListFn[Pipe TaskListData, Ctx TaskListData] func(*TaskList[Pipe, Ctx], *cli.Context) error
)

func (t *TaskList[Pipe, Ctx]) New(a *App, pipe *Pipe, context *Ctx) *TaskList[Pipe, Ctx] {
	t.App = a
	t.Log = a.Log
	t.Channnel = &a.Channel
	t.Lock = &sync.RWMutex{}

	t.Pipe = pipe
	t.Context = context

	t.runBefore = func(tl *TaskList[Pipe, Ctx], ctx *cli.Context) error {
		return nil
	}
	t.runAfter = func(tl *TaskList[Pipe, Ctx], ctx *cli.Context) error {
		return nil
	}

	t.flocContext = floc.NewContext()
	t.Control = floc.NewControl(t.flocContext)

	return t
}

func (t *TaskList[Pipe, Ctx]) GetTasks() floc.Job {
	return t.Tasks
}

func (t *TaskList[Pipe, Ctx]) SetTasks(tasks floc.Job) *TaskList[Pipe, Ctx] {
	t.Tasks = tasks

	return t
}

func (t *TaskList[Pipe, Ctx]) ShouldRunBefore(fn taskListFn[Pipe, Ctx]) *TaskList[Pipe, Ctx] {
	t.runBefore = fn

	return t
}

func (t *TaskList[Pipe, Ctx]) ShouldRunAfter(fn taskListFn[Pipe, Ctx]) *TaskList[Pipe, Ctx] {
	t.runAfter = fn

	return t
}

func (t *TaskList[Pipe, Ctx]) Validate(data TaskListData) error {
	if err := defaults.Set(&data); err != nil {
		return fmt.Errorf("Can not set defaults: %s", err)
	}

	validate := validator.New()

	err := validate.Struct(&data)

	if err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			error := fmt.Sprintf(
				`"%s" field failed validation: %s`,
				err.Namespace(),
				err.Tag(),
			)

			t.Log.Errorln(error)
		}

		return errors.New("Validation failed.")
	}

	return nil
}

func (t *TaskList[Pipe, Ctx]) Run(c *cli.Context) error {
	if err := t.Validate(t.Pipe); err != nil {
		return err
	}

	if err := t.Validate(t.Context); err != nil {
		return err
	}

	if err := t.runBefore(t, c); err != nil {
		return err
	}

	if _, _, err := floc.RunWith(t.flocContext, t.Control, t.Tasks); err != nil {
		return err
	}

	if err := t.runAfter(t, c); err != nil {
		return err
	}

	return nil
}

func (t *TaskList[Pipe, Ctx]) Job(c *cli.Context) floc.Job {
	return func(ctx floc.Context, ctrl floc.Control) error {
		t.flocContext = ctx
		t.Control = ctrl

		return t.Run(c)
	}
}
