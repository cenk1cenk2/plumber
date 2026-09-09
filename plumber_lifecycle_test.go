package plumber_test

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/cenk1cenk2/plumber/v7"
	plumbertests "github.com/cenk1cenk2/plumber/v7/tests"
	"github.com/urfave/cli/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type unnumberedSignal struct{}

func (unnumberedSignal) Signal() {}

func (unnumberedSignal) String() string {
	return "unnumbered"
}

var _ = Describe("plumber lifecycle", func() {
	BeforeEach(func() {
		plumbertests.WithoutEnvironment("CI", "CLI_ARGS", "DEBUG", "ENV_FILE")
	})

	Describe("Cli parsing and setup", func() {
		It("should append CLI_ARGS after process args so command flags parse", func() {
			plumbertests.WithEnvironment(map[string]string{
				"CLI_ARGS": "--name from-cli-args",
			})
			plumbertests.WithArgs("cli-args-test", "run")
			parsedName := ""
			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "cli-args-test",
					Commands: []*cli.Command{
						{
							Name: "run",
							Flags: []cli.Flag{
								&cli.StringFlag{Name: "name"},
							},
							Action: func(_ context.Context, command *cli.Command) error {
								parsedName = command.String("name")

								return nil
							},
						},
					},
				}
			})

			fixture.Plumber.Run()

			Expect(parsedName).To(Equal("from-cli-args"))
		})

		It("should parse root flags and environment-backed flags before actions", func() {
			plumbertests.WithArgs("setup-test", "--ci", "--log-level", "warn", "--debug", "run")
			var debug bool
			var ci bool
			var level plumber.LogLevel
			fixture := plumbertests.NewPlumber(func(app *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "setup-test",
					Commands: []*cli.Command{
						{
							Name: "run",
							Action: func(_ context.Context, _ *cli.Command) error {
								debug = app.Environment.Debug
								ci = app.Environment.CI
								level = app.GetLoggerLevel()

								return nil
							},
						},
					},
				}
			})

			fixture.Plumber.Run()

			Expect(debug).To(BeTrue())
			Expect(ci).To(BeTrue())
			Expect(level).To(Equal(plumber.LogLevelDebug))
		})

		It("should run the wrapped Cli Before hook before actions", func(ctx SpecContext) {
			plumbertests.WithArgs("before-test", "run")
			order := []string{}
			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "before-test",
					Before: func(ctx context.Context, _ *cli.Command) (context.Context, error) {
						order = append(order, "before")

						return ctx, nil
					},
					Commands: []*cli.Command{
						{
							Name: "run",
							Action: func(_ context.Context, _ *cli.Command) error {
								order = append(order, "action")

								return nil
							},
						},
					},
				}
			})

			fixture.Plumber.Run()

			Expect(order).To(Equal([]string{"before", "action"}))
		})

		It("should run configured greeters before Cli actions", func() {
			plumbertests.WithArgs("greeter-test", "run")
			order := []string{}
			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "greeter-test",
					Commands: []*cli.Command{
						{
							Name: "run",
							Action: func(_ context.Context, _ *cli.Command) error {
								order = append(order, "action")

								return nil
							},
						},
					},
				}
			})
			fixture.Plumber.SetGreeter(func(_ *plumber.Plumber) error {
				order = append(order, "greeter")

				return nil
			})

			fixture.Plumber.Run()

			Expect(order).To(Equal([]string{"greeter", "action"}))
		})

	})

	Describe("configuration and jobs", func() {
		It("should apply Set hooks and expose the configured delimiter to new tasks", func() {
			fixture := plumbertests.NewPlumber()

			result := fixture.Plumber.Set(func(app *plumber.Plumber) error {
				app.SetDelimiter("|")
				app.SetTerminatorTimeout(time.Millisecond)

				return nil
			})

			Expect(result).To(BeIdenticalTo(fixture.Plumber))
			Expect(fixture.NewTaskList("configured").CreateTask("deploy", "", "prepare").Name).To(Equal("deploy|prepare"))
		})

		It("should accept empty runtimes by restoring the default runtime", func() {
			fixture := plumbertests.NewPlumber()

			Expect(fixture.Plumber.SetRuntime(plumber.Runtime{})).To(BeIdenticalTo(fixture.Plumber))
		})

		DescribeTable("should run jobs through plumber",
			func(job plumber.Job, expected error) {
				fixture := plumbertests.NewPlumber()

				err := fixture.Plumber.RunJobs(job)
				if expected == nil {
					Expect(err).ToNot(HaveOccurred())

					return
				}

				Expect(err).To(MatchError(expected))
			},
			Entry("nil jobs", nil, nil),
			Entry("successful jobs", plumber.CreateJob(func() error {
				return nil
			}), nil),
			Entry("failed jobs", plumber.CreateJob(func() error {
				return errors.New("job failed")
			}), errors.New("job failed")),
		)
	})

	Describe("graceful shutdown", func() {
		It("should yield nil from RunJobs when a fatal error shuts the application down while a job of its own still fails", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()

			Expect(fixture.Plumber.RunJobs(plumber.CreateJob(func() error {
				return errors.New("job failed")
			}))).To(MatchError("job failed"))

			Expect(fixture.Plumber.RunJobs(func(ctx context.Context) error {
				go fixture.Plumber.SendFatal(nil, errors.New("fatal error"))

				<-ctx.Done()

				return fmt.Errorf("signal: killed")
			})).To(Succeed())

			Eventually(fixture.ExitCodes).Should(Equal([]int{1}))
		}, SpecTimeout(time.Second*10))

		It("should propagate the error of a command of a task that can not start", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()
			runner := plumbertests.NewTestingCommandRunner()
			runner.Add(plumbertests.TestingCommandResponse{
				Name:   "kustomize",
				Result: &plumber.CommandResult{},
				Err:    errors.New(`exec: "kustomize": executable file not found in $PATH`),
			})

			tl := fixture.NewTaskList("version").
				SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()}).
				Set(func(tl *plumber.TaskList) plumber.Job {
					return tl.CreateTask("version").
						Set(func(_ context.Context, t *plumber.Task) error {
							t.CreateCommand("kustomize", "version").AddSelfToTheTask()

							return nil
						}).
						ShouldRunAfter(func(ctx context.Context, t *plumber.Task) error {
							return t.RunCommandJobAsJobSequence(ctx)
						}).
						Job()
				})

			Expect(fixture.Plumber.RunJobs(plumber.CombineTaskLists(tl))).
				To(MatchError(ContainSubstring(`exec: "kustomize": executable file not found in $PATH`)))
			Expect(fixture.ExitCodes()).To(BeEmpty())
		}, SpecTimeout(time.Second*10))

		It("should exit with a failure code when the action of the application fails", func(_ SpecContext) {
			plumbertests.WithArgs("failing-test", "run")
			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "failing-test",
					Commands: []*cli.Command{
						{
							Name: "run",
							Action: func(_ context.Context, _ *cli.Command) error {
								return errors.New(`exec: "kustomize": executable file not found in $PATH`)
							},
						},
					},
				}
			})

			fixture.Plumber.Run()

			Expect(fixture.ExitCodes()).To(Equal([]int{1}))
		}, SpecTimeout(time.Second*10))

		It("should resume the flow when a failing task is guarded to resume", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()

			resumed := false

			task := fixture.NewTaskList("guarded").CreateTask("failing").
				Set(func(_ context.Context, _ *plumber.Task) error {
					return errors.New("task failed")
				})

			Expect(fixture.Plumber.RunJobs(plumber.JobSequence(
				plumber.GuardResume(task.Job()),
				plumber.CreateJob(func() error {
					resumed = true

					return nil
				}),
			))).To(Succeed())

			Expect(resumed).To(BeTrue())
			Expect(fixture.ExitCodes()).To(BeEmpty())
		}, SpecTimeout(time.Second*10))

		It("should yield nil from RunJobs when the terminator shuts the application down", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()
			fixture.Plumber.EnableTerminator()
			fixture.NewTaskList("terminating")

			Expect(fixture.Plumber.RunJobs(func(ctx context.Context) error {
				go fixture.Plumber.SendTerminate(syscall.SIGTERM, 0)

				<-ctx.Done()

				return fmt.Errorf("signal: killed")
			})).To(Succeed())
		}, SpecTimeout(time.Second*10))

		It("should surface the error of a flow that is cancelled by whoever runs it", func(spec SpecContext) {
			fixture := plumbertests.NewPlumber()

			ctx, cancel := context.WithCancel(spec)
			defer cancel()

			Expect(fixture.Plumber.RunJobsWith(ctx, func(ctx context.Context) error {
				cancel()

				<-ctx.Done()

				return ctx.Err()
			})).To(MatchError(context.Canceled))
		}, SpecTimeout(time.Second*10))
	})

	Describe("terminator", func() {
		It("should exit with the code that is requested through the terminator", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()
			fixture.Plumber.EnableTerminator()

			fixture.Plumber.Terminate(9)

			Expect(fixture.ExitCodes()).To(Equal([]int{9}))
		}, SpecTimeout(time.Second*10))

		It("should exit with the interrupt code when the application is terminated through a signal", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()
			fixture.Plumber.EnableTerminator()

			fixture.Plumber.SendTerminate(syscall.SIGINT, plumber.SignalExitCode(syscall.SIGINT))

			Expect(fixture.ExitCodes()).To(Equal([]int{130}))
		}, SpecTimeout(time.Second*10))

		DescribeTable("should map a signal to the exit code that the shell reports for it",
			func(_ SpecContext, sig syscall.Signal, code int) {
				fixture := plumbertests.NewPlumber()
				fixture.Plumber.EnableTerminator()

				Expect(plumber.SignalExitCode(sig)).To(Equal(code))

				fixture.Plumber.SendTerminate(sig, plumber.SignalExitCode(sig))

				Expect(fixture.ExitCodes()).To(Equal([]int{code}))
			},
			Entry("SIGINT", syscall.SIGINT, 130),
			Entry("SIGTERM", syscall.SIGTERM, 143),
			Entry("SIGQUIT", syscall.SIGQUIT, 131),
		)

		It("should fall back to the generic failure code for a signal without a number", func(_ SpecContext) {
			Expect(plumber.SignalExitCode(unnumberedSignal{})).To(Equal(1))
		}, SpecTimeout(time.Second*10))

		It("should exit immediately when nothing is registered to the terminator", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()
			fixture.Plumber.EnableTerminator()
			fixture.Plumber.SetTerminatorTimeout(time.Minute)

			started := time.Now()

			fixture.Plumber.SendTerminate(syscall.SIGTERM, 0)

			Expect(time.Since(started)).To(BeNumerically("<", time.Second))
			Expect(fixture.ExitCodes()).To(Equal([]int{0}))
		}, SpecTimeout(time.Second*10))

		It("should ignore the terminate signals that come after the first one", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()
			fixture.Plumber.EnableTerminator()

			fixture.Plumber.SendTerminate(syscall.SIGTERM, plumber.SignalExitCode(syscall.SIGTERM))
			fixture.Plumber.SendTerminate(syscall.SIGINT, plumber.SignalExitCode(syscall.SIGINT))

			Expect(fixture.ExitCodes()).To(Equal([]int{143}))
		}, SpecTimeout(time.Second*10))

		It("should not run the terminator hook of a task that is already done running", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()
			fixture.Plumber.EnableTerminator()

			hooked := make(chan bool, 1)

			task := fixture.NewTaskList("terminator").CreateTask("done").
				SetOnTerminator(func(_ context.Context, _ *plumber.Task) error {
					hooked <- true

					return nil
				}).
				EnableTerminator()

			Expect(fixture.Plumber.RunJobs(task.Job())).To(Succeed())

			fixture.Plumber.SendTerminate(syscall.SIGTERM, 0)

			Expect(hooked).ToNot(Receive())
			Expect(fixture.ExitCodes()).To(Equal([]int{0}))
		}, SpecTimeout(time.Second*10))

		It("should force the exit when the hooks of the terminator do not finish in time", func(_ SpecContext) {
			fixture := plumbertests.NewPlumber()
			fixture.Plumber.EnableTerminator()
			fixture.Plumber.SetTerminatorTimeout(time.Millisecond * 50)

			running := make(chan bool)
			release := make(chan bool)
			done := make(chan error, 1)

			task := fixture.NewTaskList("terminator").CreateTask("hanging").
				Set(func(ctx context.Context, _ *plumber.Task) error {
					close(running)

					<-ctx.Done()

					return ctx.Err()
				}).
				SetOnTerminator(func(_ context.Context, _ *plumber.Task) error {
					<-release

					return nil
				}).
				EnableTerminator()

			go func() {
				defer GinkgoRecover()

				done <- fixture.Plumber.RunJobs(task.Job())
			}()

			<-running

			fixture.Plumber.SendTerminate(syscall.SIGTERM, 5)

			Expect(fixture.ExitCodes()).To(Equal([]int{5}))

			close(release)

			Eventually(done).Should(Receive(BeNil()))
		}, SpecTimeout(time.Second*10))
	})
})
