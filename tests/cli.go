package tests

import (
	"context"
	"fmt"

	"github.com/cenk1cenk2/plumber/v6"
	. "github.com/onsi/ginkgo/v2"
	"github.com/urfave/cli/v3"
)

type TaskListFactory func(*plumber.Plumber, *cli.Command) *plumber.TaskList

type TaskListCli struct {
	AppName            string
	CommandName        string
	Args               []string
	Environment        map[string]string
	WithoutEnvironment []string
	Flags              []cli.Flag
	Arguments          []cli.Argument
	Before             cli.BeforeFunc
	Runner             plumber.CommandRunner
	TaskLists          []TaskListFactory
}

type TaskListCliFixture struct {
	*PlumberFixture
	Command   *cli.Command
	TaskLists []*plumber.TaskList

	args               []string
	environment        map[string]string
	withoutEnvironment []string
}

func (f *PlumberFixture) RunCli(args ...string) error {
	GinkgoHelper()

	return f.RunCliContext(context.Background(), args...)
}

func (f *PlumberFixture) RunCliContext(ctx context.Context, args ...string) error {
	GinkgoHelper()

	if len(args) == 0 {
		args = []string{f.Plumber.Cli.Name}
	}

	return f.Plumber.Cli.Run(ctx, args)
}

func NewTaskListCli(spec TaskListCli) *TaskListCliFixture {
	GinkgoHelper()

	if spec.AppName == "" {
		spec.AppName = "plumber-test"
	}

	if spec.CommandName == "" {
		spec.CommandName = "run"
	}

	result := &TaskListCliFixture{
		args:               spec.Args,
		environment:        spec.Environment,
		withoutEnvironment: spec.WithoutEnvironment,
	}

	fixture := NewPlumber(func(app *plumber.Plumber) *cli.Command {
		result.Command = &cli.Command{
			Name: spec.AppName,
			Commands: []*cli.Command{
				{
					Name:      spec.CommandName,
					Flags:     spec.Flags,
					Arguments: spec.Arguments,
					Before:    spec.Before,
					Action: func(_ context.Context, command *cli.Command) error {
						if len(spec.TaskLists) == 0 {
							return fmt.Errorf("at least one task list factory is required")
						}

						result.TaskLists = make([]*plumber.TaskList, 0, len(spec.TaskLists))
						for _, factory := range spec.TaskLists {
							tl := factory(app, command)
							result.TaskLists = append(result.TaskLists, tl)
						}

						return app.RunJobs(plumber.CombineTaskLists(result.TaskLists...))
					},
				},
			},
		}

		return result.Command
	})

	result.PlumberFixture = fixture
	if spec.Runner != nil {
		result.Plumber.SetCommandRunner(spec.Runner)
	}

	if len(result.args) == 0 {
		result.args = []string{spec.AppName, spec.CommandName}
	}

	return result
}

func (f *TaskListCliFixture) Run(args ...string) error {
	GinkgoHelper()

	return f.RunContext(context.Background(), args...)
}

func (f *TaskListCliFixture) RunContext(ctx context.Context, args ...string) error {
	GinkgoHelper()

	if len(f.withoutEnvironment) > 0 {
		WithoutEnvironment(f.withoutEnvironment...)
	}

	if len(f.environment) > 0 {
		WithEnvironment(f.environment)
	}

	if len(args) == 0 {
		args = f.args
	}

	return f.PlumberFixture.RunCliContext(ctx, args...)
}
