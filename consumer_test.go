package plumber_test

import (
	"context"
	"sync"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	"github.com/urfave/cli/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("consumer-shaped flows", func() {
	It("should let consumers test CLI flags arguments commands and generated subtasks with a runtime command runner", func() {
		runner := plumbertests.NewTestingCommandRunner().
			AddResponses(
				plumbertests.TestingCommandResponse{Name: "corepack", Args: []string{"enable"}},
				plumbertests.TestingCommandResponse{Name: "npm", Args: []string{"run", "build", "--", "--production"}},
			)
		workDir := plumbertests.TempDir()
		var subtaskLock sync.Mutex
		subtaskOrder := []string{}
		fixture := plumbertests.NewPlumber(func(p *plumber.Plumber) *cli.Command {
			return &cli.Command{
				Name: "consumer",
				Commands: []*cli.Command{
					{
						Name: "run",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "script", Value: "test"},
							&cli.StringFlag{Name: "cwd", Value: "."},
							&cli.StringSliceFlag{Name: "repository"},
						},
						Arguments: []cli.Argument{
							&cli.StringArgs{Name: "args", Max: -1},
						},
						Action: func(ctx context.Context, command *cli.Command) error {
							setup := plumber.NewTaskList(p)
							setup.Name = "setup"
							setup.Set(func(tl *plumber.TaskList) plumber.Job {
								return tl.CreateTask("corepack").
									Set(func(t *plumber.Task) error {
										t.CreateCommand("corepack", "enable").AddSelfToTheTask()

										return nil
									}).
									ShouldRunAfter(func(t *plumber.Task) error {
										return t.RunCommandJobAsJobSequence()
									}).
									Job()
							})

							run := plumber.NewTaskList(p)
							run.Name = "run"
							run.Set(func(tl *plumber.TaskList) plumber.Job {
								return plumber.JobSequence(
									tl.CreateTask("npm", command.String("script")).
										Set(func(t *plumber.Task) error {
											npm := t.CreateCommand("npm", "run", command.String("script"), "--")
											npm.AppendArgs(command.StringArgs("args")...)
											npm.SetDir(command.String("cwd"))
											npm.AddSelfToTheTask()

											return nil
										}).
										ShouldRunAfter(func(t *plumber.Task) error {
											return t.RunCommandJobAsJobSequence()
										}).
										Job(),
									tl.CreateTask("repositories").
										Set(func(t *plumber.Task) error {
											for _, repository := range command.StringSlice("repository") {
												repository := repository
												t.CreateSubtask(repository).
													Set(func(_ *plumber.Task) error {
														subtaskLock.Lock()
														subtaskOrder = append(subtaskOrder, repository)
														subtaskLock.Unlock()

														return nil
													}).
													AddSelfToTheParentAsParallel()
											}

											return nil
										}).
										ShouldRunAfter(func(t *plumber.Task) error {
											return t.RunSubtasks()
										}).
										Job(),
								)
							})

							_ = ctx

							return p.RunJobs(plumber.CombineTaskLists(setup, run))
						},
					},
				},
			}
		})
		fixture.Plumber.SetCommandRunner(runner.Runner())
		plumbertests.WithArgs(
			"consumer",
			"run",
			"--script",
			"build",
			"--cwd",
			workDir,
			"--repository",
			"api",
			"--repository",
			"web",
			"--",
			"--production",
		)

		fixture.Plumber.Run()

		Expect(runner.InvocationNames()).To(Equal([]string{"corepack", "npm"}))
		invocations := runner.Invocations()
		Expect(invocations[1].Dir).To(Equal(workDir))
		Expect(invocations[1].TaskName).To(Equal("npm:build"))
		Expect(invocations[1].TaskListName).To(Equal("run"))
		Expect(subtaskOrder).To(ConsistOf("api", "web"))
	})
})
