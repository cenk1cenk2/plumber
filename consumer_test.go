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
