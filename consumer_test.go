package plumber_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	"github.com/urfave/cli/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type pipeShapeCase struct {
	prepare func(*plumbertests.PlumberFixture, *plumbertests.TestingCommandRunner) (plumber.Job, func())
}

type pipeCliConditionCase struct {
	args               func(string) []string
	environment        func(string) map[string]string
	expectedDisabled   bool
	expectedPackages   []string
	expectedCwd        func(string) string
	expectedScriptArgs []string
}

var _ = Describe("consumer-shaped flows", func() {
	It("should let consumers test Cli flags arguments commands and generated subtasks with a runtime command runner", func() {
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

	DescribeTable("should let consumers drive task-list conditions through urfave Cli helpers",
		func(tc pipeCliConditionCase) {
			type packageConfig struct {
				Enabled    bool
				Cwd        string
				Packages   []string
				ScriptArgs []string
			}

			cwd := plumbertests.TempDir()
			config := &packageConfig{}
			runner := plumbertests.NewTestingCommandRunner()
			fixture := plumbertests.NewTaskListCli(plumbertests.TaskListCli{
				AppName:            "consumer-config",
				Args:               tc.args(cwd),
				Environment:        tc.environment(cwd),
				WithoutEnvironment: []string{"PACKAGES_ENABLED", "PACKAGES_NODE", "PACKAGES_CWD"},
				Runner:             runner.Runner(),
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:        "packages.enabled",
						Value:       true,
						Sources:     cli.NewValueSourceChain(cli.EnvVar("PACKAGES_ENABLED")),
						Destination: &config.Enabled,
					},
					&cli.StringFlag{
						Name:        "packages.cwd",
						Value:       ".",
						Sources:     cli.NewValueSourceChain(cli.EnvVar("PACKAGES_CWD")),
						Destination: &config.Cwd,
					},
					&cli.StringSliceFlag{
						Name:        "packages.node",
						Sources:     cli.NewValueSourceChain(cli.EnvVar("PACKAGES_NODE")),
						Destination: &config.Packages,
					},
				},
				Arguments: []cli.Argument{
					&cli.StringArgs{
						Name:        "script-args",
						Max:         -1,
						Destination: &config.ScriptArgs,
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
								return tl.CreateTask("packages", "node").
									Set(func(parent *plumber.Task) error {
										for _, packageName := range config.Packages {
											packageName := packageName
											parent.CreateSubtask(packageName).
												Set(func(task *plumber.Task) error {
													task.CreateCommand("npm", "add", packageName).
														AppendArgs(config.ScriptArgs...).
														SetDir(config.Cwd).
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

			Expect(config.Packages).To(Equal(tc.expectedPackages))
			Expect(config.ScriptArgs).To(Equal(tc.expectedScriptArgs))
			Expect(fixture.TaskLists).To(HaveLen(1))
			Expect(fixture.TaskLists[0].IsDisabled()).To(Equal(tc.expectedDisabled))

			invocations := runner.Invocations()
			if tc.expectedDisabled {
				Expect(invocations).To(BeEmpty())

				return
			}

			Expect(invocations).To(HaveLen(len(tc.expectedPackages)))
			byPackage := map[string]plumber.CommandInvocation{}
			for _, invocation := range invocations {
				Expect(invocation.Name).To(Equal("npm"))
				Expect(invocation.Dir).To(Equal(tc.expectedCwd(cwd)))
				Expect(invocation.Args).To(HaveLen(2 + len(tc.expectedScriptArgs)))
				byPackage[invocation.Args[1]] = invocation
			}
			for _, packageName := range tc.expectedPackages {
				Expect(byPackage).To(HaveKey(packageName))
				Expect(byPackage[packageName].Args).To(Equal(append([]string{"add", packageName}, tc.expectedScriptArgs...)))
			}
		},
		Entry("disabled from env-sourced config", pipeCliConditionCase{
			args: func(_ string) []string {
				return []string{"consumer-config", "run"}
			},
			environment: func(_ string) map[string]string {
				return map[string]string{
					"PACKAGES_ENABLED": "false",
					"PACKAGES_NODE":    "api,web",
				}
			},
			expectedDisabled:   true,
			expectedPackages:   []string{"api", "web"},
			expectedScriptArgs: []string{},
			expectedCwd: func(_ string) string {
				return "."
			},
		}),
		Entry("enabled by flags with Cli values overriding env sources", pipeCliConditionCase{
			args: func(cwd string) []string {
				return []string{
					"consumer-config",
					"run",
					"--packages.enabled",
					"--packages.cwd",
					cwd,
					"--packages.node",
					"api",
					"--packages.node",
					"web",
					"--",
					"--save-dev",
				}
			},
			environment: func(_ string) map[string]string {
				return map[string]string{
					"PACKAGES_ENABLED": "false",
					"PACKAGES_NODE":    "ignored",
				}
			},
			expectedPackages:   []string{"api", "web"},
			expectedScriptArgs: []string{"--save-dev"},
			expectedCwd: func(cwd string) string {
				return cwd
			},
		}),
	)

	DescribeTable("should exercise pipe task shapes through runtime command runners",
		func(tc pipeShapeCase) {
			fixture := plumbertests.NewPlumber()
			runner := plumbertests.NewTestingCommandRunner()
			fixture.Plumber.SetCommandRunner(runner.Runner())

			job, assert := tc.prepare(fixture, runner)

			Expect(fixture.Plumber.RunJobs(job)).To(Succeed())
			assert()
		},
		Entry("setup tasks that read recorded command streams", pipeShapeCase{
			prepare: func(fixture *plumbertests.PlumberFixture, runner *plumbertests.TestingCommandRunner) (plumber.Job, func()) {
				runner.AddResponses(
					plumbertests.TestingCommandResponse{Name: "node", Stdout: "v20.0.0\n"},
					plumbertests.TestingCommandResponse{Name: "pnpm", Stdout: "9.0.0\n"},
				)
				versions := []string{}
				var lock sync.Mutex
				tl := fixture.NewTaskList("setup").
					Set(func(tl *plumber.TaskList) plumber.Job {
						return tl.CreateTask("version").
							Set(func(t *plumber.Task) error {
								for _, spec := range []struct {
									name string
									args []string
								}{
									{name: "node", args: []string{"--version"}},
									{name: "pnpm", args: []string{"--version"}},
								} {
									spec := spec
									t.CreateCommand(spec.name, spec.args...).
										EnableStreamRecording().
										ShouldRunAfter(func(c *plumber.Command) error {
											lock.Lock()
											versions = append(versions, strings.TrimSpace(c.GetCombinedStream()[0]))
											lock.Unlock()

											return nil
										}).
										AddSelfToTheTask()
								}

								return nil
							}).
							ShouldRunAfter(func(t *plumber.Task) error {
								return t.RunCommandJobAsJobParallel()
							}).
							Job()
					})

				return tl.Job(), func() {
					Expect(runner.InvocationNames()).To(ConsistOf("node", "pnpm"))
					Expect(versions).To(ConsistOf("v20.0.0", "9.0.0"))
				}
			},
		}),
		Entry("login tasks that send secrets through stdin", pipeShapeCase{
			prepare: func(fixture *plumbertests.PlumberFixture, runner *plumbertests.TestingCommandRunner) (plumber.Job, func()) {
				runner.Add(plumbertests.TestingCommandResponse{Name: "helm"})
				tl := fixture.NewTaskList("login").
					Set(func(tl *plumber.TaskList) plumber.Job {
						return tl.CreateTask("registry").
							Set(func(t *plumber.Task) error {
								t.CreateCommand("helm", "registry", "login", "registry.example", "--username", "ci", "--password-stdin").
									SetStdin(func(_ *plumber.Command) io.Reader {
										return strings.NewReader("secret-token\n")
									}).
									AddSelfToTheTask()

								return nil
							}).
							ShouldRunAfter(func(t *plumber.Task) error {
								return t.RunCommandJobAsJobSequence()
							}).
							Job()
					})

				return tl.Job(), func() {
					invocation, ok := runner.LastInvocation()
					Expect(ok).To(BeTrue())
					stdin, err := plumbertests.ReadInvocationStdin(invocation)
					Expect(err).ToNot(HaveOccurred())
					Expect(stdin).To(Equal("secret-token\n"))
				}
			},
		}),
		Entry("build matrix tasks with generated parallel subtasks", pipeShapeCase{
			prepare: func(fixture *plumbertests.PlumberFixture, runner *plumbertests.TestingCommandRunner) (plumber.Job, func()) {
				runner.AddResponses(
					plumbertests.TestingCommandResponse{Name: "go", Match: func(invocation plumber.CommandInvocation) bool {
						return invocation.TaskName == "build:api:linux/amd64" && strings.Contains(strings.Join(invocation.Env, "\x00"), "GOOS=linux")
					}},
					plumbertests.TestingCommandResponse{Name: "go", Match: func(invocation plumber.CommandInvocation) bool {
						return invocation.TaskName == "build:api:darwin/arm64" && strings.Contains(strings.Join(invocation.Env, "\x00"), "GOOS=darwin")
					}},
				)
				cwd := plumbertests.TempDir()
				targets := []struct {
					os   string
					arch string
				}{
					{os: "linux", arch: "amd64"},
					{os: "darwin", arch: "arm64"},
				}
				tl := fixture.NewTaskList("build").
					Set(func(tl *plumber.TaskList) plumber.Job {
						return tl.CreateTask("build").
							Set(func(parent *plumber.Task) error {
								for _, target := range targets {
									target := target
									parent.CreateSubtask("api", target.os+"/"+target.arch).
										Set(func(t *plumber.Task) error {
											t.CreateCommand("go", "build", "-mod=vendor").
												SetDir(cwd).
												AppendEnvironment(map[string]string{
													"GOOS":   target.os,
													"GOARCH": target.arch,
												}).
												AddSelfToTheTask()

											return nil
										}).
										ShouldRunAfter(func(t *plumber.Task) error {
											return t.RunCommandJobAsJobParallel()
										}).
										AddSelfToTheParentAsParallel()
								}

								return nil
							}).
							ShouldRunAfter(func(t *plumber.Task) error {
								return t.RunSubtasks()
							}).
							Job()
					})

				return tl.Job(), func() {
					invocations := runner.Invocations()
					Expect(invocations).To(HaveLen(2))
					Expect([]string{invocations[0].TaskName, invocations[1].TaskName}).To(ConsistOf(
						"build:api:linux/amd64",
						"build:api:darwin/arm64",
					))
					for _, invocation := range invocations {
						Expect(invocation.Dir).To(Equal(cwd))
					}
				}
			},
		}),
		Entry("plan tasks that retry transient command failures", pipeShapeCase{
			prepare: func(fixture *plumbertests.PlumberFixture, runner *plumbertests.TestingCommandRunner) (plumber.Job, func()) {
				failure := plumbertests.TestingCommandFailure(1)
				runner.AddResponses(
					plumbertests.TestingCommandResponse{Name: "terraform", Result: &failure},
					plumbertests.TestingCommandResponse{Name: "terraform"},
				)
				retry := &plumber.CommandRetry{Tries: 1, Delay: time.Millisecond}
				tl := fixture.NewTaskList("terraform").
					Set(func(tl *plumber.TaskList) plumber.Job {
						return tl.CreateTask("plan").
							Set(func(t *plumber.Task) error {
								t.CreateCommand("terraform", "plan", "-input=false").
									SetRetries(retry).
									AddSelfToTheTask()

								return nil
							}).
							ShouldRunAfter(func(t *plumber.Task) error {
								return t.RunCommandJobAsJobSequence()
							}).
							Job()
					})

				return tl.Job(), func() {
					Expect(runner.InvocationNames()).To(Equal([]string{"terraform", "terraform"}))
					Expect(retry.Tries).To(BeEquivalentTo(0))
				}
			},
		}),
	)
})
