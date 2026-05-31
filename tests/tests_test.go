package tests_test

import (
	"context"
	"os"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
	"github.com/urfave/cli/v3"
)

var _ = Describe("test helpers", func() {
	It("should create a plumber fixture with ginkgo trace logging", func() {
		fixture := plumbertests.NewPlumber()

		fixture.Plumber.Log.Info("hello")

		Expect(fixture.Plumber.Cli.Name).To(Equal("plumber-test"))
		Expect(fixture.Plumber.Log.Out).To(Equal(GinkgoWriter))
		Expect(fixture.Plumber.Log.GetLevel()).To(Equal(logrus.TraceLevel))
	})

	It("should set process arguments for the current spec", func() {
		plumbertests.WithArgs("plumber", "test")

		Expect(os.Args).To(Equal([]string{"plumber", "test"}))
	})

	It("should create strict mockery command runners for the current spec", func() {
		runner := plumbertests.NewMockCommandRunner()
		result := plumbertests.TestingCommandSuccess()
		runner.EXPECT().
			Run(
				mock.Anything,
				mock.MatchedBy(func(invocation plumber.CommandInvocation) bool {
					return invocation.Name == "mock"
				}),
				mock.Anything,
			).
			Return(result, nil).
			Once()

		actual, err := runner.Run(
			context.Background(),
			plumber.CommandInvocation{Name: "mock"},
			plumber.CommandRuntime{},
		)

		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(result))
	})

	It("should set environment variables for the current spec", func() {
		plumbertests.WithEnvironment(map[string]string{
			"PLUMBER_TEST_HELPER": "enabled",
		})

		Expect(os.Getenv("PLUMBER_TEST_HELPER")).To(Equal("enabled"))
	})

	It("should unset environment variables for the current spec", func() {
		plumbertests.WithEnvironment(map[string]string{
			"PLUMBER_TEST_HELPER": "enabled",
		})

		plumbertests.WithoutEnvironment("PLUMBER_TEST_HELPER")

		_, existed := os.LookupEnv("PLUMBER_TEST_HELPER")
		Expect(existed).To(BeFalse())
	})

	It("should prepend paths for the current spec", func() {
		previousPath := os.Getenv("PATH")

		plumbertests.WithPath("/tmp/plumber-bin")

		Expect(os.Getenv("PATH")).To(HavePrefix("/tmp/plumber-bin" + string(os.PathListSeparator)))
		Expect(os.Getenv("PATH")).To(ContainSubstring(previousPath))
	})

	It("should change the working directory for the current spec", func() {
		dir := plumbertests.TempDir()
		previousDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())

		Expect(plumbertests.WithWorkingDirectory(dir)).To(Equal(previousDir))

		currentDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		Expect(currentDir).To(Equal(dir))
	})

	It("should create and enter temporary working directories", func() {
		previousDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())

		dir := plumbertests.WithTempWorkingDirectory("nested")

		currentDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		Expect(currentDir).To(Equal(dir))
		Expect(currentDir).ToNot(Equal(previousDir))
	})

	It("should run urfave Cli semantics with explicit argv and destinations", func() {
		type helperConfig struct {
			Enabled      bool
			Root         string
			Repositories []string
			Args         []string
		}

		config := &helperConfig{}
		runner := plumbertests.NewTestingCommandRunner()
		previousArgs := append([]string{}, os.Args...)
		fixture := plumbertests.NewTaskListCli(plumbertests.TaskListCli{
			AppName: "helper",
			Args: []string{
				"helper",
				"run",
				"--enabled",
				"--root",
				"from-cli",
				"--repository",
				"api",
				"--repository",
				"web",
				"--",
				"--production",
			},
			Environment: map[string]string{
				"HELPER_ROOT":       "from-env",
				"HELPER_REPOSITORY": "ignored",
			},
			Runner: runner.Runner(),
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:        "enabled",
					Sources:     cli.NewValueSourceChain(cli.EnvVar("HELPER_ENABLED")),
					Destination: &config.Enabled,
				},
				&cli.StringFlag{
					Name:        "root",
					Value:       ".",
					Sources:     cli.NewValueSourceChain(cli.EnvVar("HELPER_ROOT")),
					Destination: &config.Root,
				},
				&cli.StringSliceFlag{
					Name:        "repository",
					Sources:     cli.NewValueSourceChain(cli.EnvVar("HELPER_REPOSITORY")),
					Destination: &config.Repositories,
				},
			},
			Arguments: []cli.Argument{
				&cli.StringArgs{
					Name:        "args",
					Max:         -1,
					Destination: &config.Args,
				},
			},
			TaskLists: []plumbertests.TaskListFactory{
				func(app *plumber.Plumber, command *cli.Command) *plumber.TaskList {
					Expect(command.String("root")).To(Equal(config.Root))
					Expect(command.StringSlice("repository")).To(Equal(config.Repositories))
					Expect(command.StringArgs("args")).To(Equal(config.Args))

					return plumber.NewTaskList(app).
						SetRuntimeDepth(1).
						ShouldDisable(func(_ *plumber.TaskList) bool {
							return !config.Enabled
						}).
						Set(func(tl *plumber.TaskList) plumber.Job {
							return tl.CreateTask("repositories").
								Set(func(parent *plumber.Task) error {
									for _, repository := range config.Repositories {
										repository := repository
										parent.CreateSubtask(repository).
											Set(func(task *plumber.Task) error {
												task.CreateCommand("build", repository).
													AppendArgs(config.Args...).
													SetDir(config.Root).
													AddSelfToTheTask()

												return task.RunCommandJobAsJobSequence()
											}).
											AddSelfToTheParentAsParallel()
									}

									return nil
								}).
								ShouldRunAfter(func(task *plumber.Task) error {
									return task.RunSubtasks()
								}).
								Job()
						})
				},
			},
		})

		Expect(fixture.Run()).To(Succeed())

		Expect(os.Args).To(Equal(previousArgs))
		Expect(config.Enabled).To(BeTrue())
		Expect(config.Root).To(Equal("from-cli"))
		Expect(config.Repositories).To(Equal([]string{"api", "web"}))
		Expect(config.Args).To(Equal([]string{"--production"}))
		Expect(fixture.TaskLists).To(HaveLen(1))
		Expect(fixture.TaskLists[0].IsDisabled()).To(BeFalse())
		Expect(runner.InvocationNames()).To(ConsistOf("build", "build"))
		for _, invocation := range runner.Invocations() {
			Expect(invocation.Dir).To(Equal("from-cli"))
			Expect(invocation.Args).To(Or(
				Equal([]string{"api", "--production"}),
				Equal([]string{"web", "--production"}),
			))
		}
	})

	It("should expose task-list conditions driven by env-sourced Cli config", func() {
		type helperConfig struct {
			Enabled      bool
			Repositories []string
		}

		config := &helperConfig{}
		runner := plumbertests.NewTestingCommandRunner()
		fixture := plumbertests.NewTaskListCli(plumbertests.TaskListCli{
			AppName: "helper-disabled",
			Environment: map[string]string{
				"HELPER_ENABLED":    "false",
				"HELPER_REPOSITORY": "api,web",
			},
			Runner: runner.Runner(),
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:        "enabled",
					Value:       true,
					Sources:     cli.NewValueSourceChain(cli.EnvVar("HELPER_ENABLED")),
					Destination: &config.Enabled,
				},
				&cli.StringSliceFlag{
					Name:        "repository",
					Sources:     cli.NewValueSourceChain(cli.EnvVar("HELPER_REPOSITORY")),
					Destination: &config.Repositories,
				},
			},
			TaskLists: []plumbertests.TaskListFactory{
				func(app *plumber.Plumber, _ *cli.Command) *plumber.TaskList {
					return plumber.NewTaskList(app).
						SetRuntimeDepth(1).
						ShouldDisable(func(_ *plumber.TaskList) bool {
							return !config.Enabled
						}).
						Set(func(tl *plumber.TaskList) plumber.Job {
							return tl.CreateTask("should-not-run").
								Set(func(task *plumber.Task) error {
									task.CreateCommand("mock").AddSelfToTheTask()

									return task.RunCommandJobAsJobSequence()
								}).
								Job()
						})
				},
			},
		})

		Expect(fixture.Run()).To(Succeed())

		Expect(config.Enabled).To(BeFalse())
		Expect(config.Repositories).To(Equal([]string{"api", "web"}))
		Expect(fixture.TaskLists).To(HaveLen(1))
		Expect(fixture.TaskLists[0].IsDisabled()).To(BeTrue())
		Expect(runner.Invocations()).To(BeEmpty())
	})
})
