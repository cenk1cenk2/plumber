package plumber

import (
	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
	. "github.com/workanator/go-floc/v3/run"

	"errors"
	"fmt"

	"github.com/creasty/defaults"
	validator "github.com/go-playground/validator/v10"
)

type TaskList[Ctx struct{}] struct {
	Tasks   []Task[Ctx]
	App     *App
	Context Ctx
	Log     *logrus.Logger
}

func (t *TaskList[Ctx]) New(a *App) *TaskList[Ctx] {
	t.App = a
	t.Log = a.Log

	t.Context = Ctx{}
	t.Tasks = []Task[Ctx]{}

	return t
}

func (t *TaskList[Ctx]) AddTasks(tasks ...Task[Ctx]) *TaskList[Ctx] {
	t.Tasks = append(t.Tasks, tasks...)

	return t
}

func (t *TaskList[Ctx]) Validate() error {
	if err := defaults.Set(&t.Context); err != nil {
		return fmt.Errorf("Can not set defaults for context: %s", err)
	}

	validate := validator.New()

	err := validate.Struct(&t.Context)

	if err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			error := fmt.Sprintf(
				"\"%s\" field failed validation: %s",
				err.Namespace(),
				err.Tag(),
			)

			t.Log.Errorln(error)
		}

		return errors.New("Context validation failed.")
	}

	return nil
}

func (t *TaskList[Ctx]) Run() error {
	if err := t.Validate(); err != nil {
		return err
	}

	floc.NewContext()
	return nil
}
