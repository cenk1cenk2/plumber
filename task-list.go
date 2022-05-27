package plumber

import (
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"

	"errors"
	"fmt"

	"github.com/creasty/defaults"
	validator "github.com/go-playground/validator/v10"
)

type TaskList[Pipe struct{}, Ctx struct{}] struct {
	Tasks floc.Job

	App *App

	Context     Ctx
	Pipe        Pipe
	Lock        *sync.RWMutex
	Log         *logrus.Logger
	Control     *AppControl
	Floc        floc.Control
	flocContext floc.Context
}

func (t *TaskList[Pipe, Ctx]) New(a *App) *TaskList[Pipe, Ctx] {
	t.App = a
	t.Log = a.Log
	t.Control = &a.Control
	t.Lock = &sync.RWMutex{}

	t.Context = Ctx{}

	t.flocContext = floc.NewContext()
	t.Floc = floc.NewControl(t.flocContext)

	return t
}

func (t *TaskList[Pipe, Ctx]) Set(tasks floc.Job) *TaskList[Pipe, Ctx] {
	t.Tasks = tasks

	return t
}

func (t *TaskList[Pipe, Ctx]) Get() floc.Job {
	return t.Tasks
}

func (t *TaskList[Pipe, Ctx]) Validate(struct{}) error {
	if err := defaults.Set(&t.Context); err != nil {
		return fmt.Errorf("Can not set defaults: %s", err)
	}

	validate := validator.New()

	err := validate.Struct(&t.Context)

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

func (t *TaskList[Pipe, Ctx]) Run() error {
	if err := t.Validate(t.Pipe); err != nil {
		return err
	}

	if err := t.Validate(t.Context); err != nil {
		return err
	}

	if _, _, err := floc.RunWith(t.flocContext, t.Floc, t.Tasks); err != nil {
		return err
	}

	return nil
}
