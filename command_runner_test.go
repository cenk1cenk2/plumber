package plumber_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cenk1cenk2/plumber/v7"
	plumbertests "github.com/cenk1cenk2/plumber/v7/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type commandRunnerScope struct {
	configure func(*plumber.Plumber, *plumber.TaskList, *plumber.Task, *plumbertests.TestingCommandRunner)
}

var _ = Describe("command runners", func() {
	var fixture *plumbertests.PlumberFixture
	BeforeEach(func() {
		fixture = plumbertests.NewPlumber()
	})

	Context("scope resolution", func() {
		It("should resolve runners from the nearest configured command scope", func(ctx SpecContext) {
			plumberRunner := plumbertests.NewTestingCommandRunner()
			taskListRunner := plumbertests.NewTestingCommandRunner()
			taskRunner := plumbertests.NewTestingCommandRunner()
			commandRunner := plumbertests.NewTestingCommandRunner()

			fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: plumberRunner.Runner()})
			tl := fixture.NewTaskList("list").SetRuntime(plumber.Runtime{CommandRunner: taskListRunner.Runner()})
			task := tl.CreateTask("task").SetRuntime(plumber.Runtime{CommandRunner: taskRunner.Runner()})
			command := task.CreateCommand("mock").SetRuntime(plumber.Runtime{CommandRunner: commandRunner.Runner()})

			Expect(command.Run(ctx)).To(Succeed())
			Expect(commandRunner.Invocations()).To(HaveLen(1))
			Expect(taskRunner.Invocations()).To(BeEmpty())
			Expect(taskListRunner.Invocations()).To(BeEmpty())
			Expect(plumberRunner.Invocations()).To(BeEmpty())
		})

		DescribeTable("should resolve runners from runtime scopes",
			func(ctx SpecContext, scope commandRunnerScope) {
				runner := plumbertests.NewTestingCommandRunner()
				tl := fixture.NewTaskList("list")
				task := tl.CreateTask("task")
				command := task.CreateCommand("mock")

				scope.configure(fixture.Plumber, tl, task, runner)

				Expect(command.Run(ctx)).To(Succeed())
				Expect(runner.InvocationNames()).To(Equal([]string{"mock"}))
			},
			Entry("task scope", commandRunnerScope{
				configure: func(_ *plumber.Plumber, _ *plumber.TaskList, task *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					task.SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})
				},
			}),
			Entry("task list scope", commandRunnerScope{
				configure: func(_ *plumber.Plumber, tl *plumber.TaskList, _ *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					tl.SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})
				},
			}),
			Entry("plumber scope", commandRunnerScope{
				configure: func(app *plumber.Plumber, _ *plumber.TaskList, _ *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					app.SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})
				},
			}),
		)

		It("should resolve command-level runtime scopes", func(ctx SpecContext) {
			runner := plumbertests.NewTestingCommandRunner()
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})

			Expect(command.Run(ctx)).To(Succeed())
			Expect(runner.InvocationNames()).To(Equal([]string{"mock"}))
		})

		It("should keep command-level runners more specific than task runtime scopes", func(ctx SpecContext) {
			taskRunner := plumbertests.NewTestingCommandRunner()
			commandRunner := plumbertests.NewTestingCommandRunner()
			task := fixture.NewTaskList("list").CreateTask("task").
				SetRuntime(plumber.Runtime{CommandRunner: taskRunner.Runner()})
			command := task.CreateCommand("mock").SetRuntime(plumber.Runtime{CommandRunner: commandRunner.Runner()})

			Expect(command.Run(ctx)).To(Succeed())
			Expect(commandRunner.InvocationNames()).To(Equal([]string{"mock"}))
			Expect(taskRunner.Invocations()).To(BeEmpty())
		})

		DescribeTable("should resolve runners from broader scopes",
			func(ctx SpecContext, scope commandRunnerScope) {
				runner := plumbertests.NewTestingCommandRunner()
				tl := fixture.NewTaskList("list")
				task := tl.CreateTask("task")

				scope.configure(fixture.Plumber, tl, task, runner)

				Expect(task.CreateCommand("mock").Run(ctx)).To(Succeed())
				Expect(runner.Invocations()).To(HaveLen(1))
				Expect(runner.Invocations()[0].TaskName).To(Equal("task"))
				Expect(runner.Invocations()[0].TaskListName).To(Equal("list"))
				Expect(runner.Invocations()[0].PlumberName).To(Equal("plumber-test"))
			},
			Entry("task scope", commandRunnerScope{
				configure: func(_ *plumber.Plumber, _ *plumber.TaskList, task *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					task.SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})
				},
			}),
			Entry("task list scope", commandRunnerScope{
				configure: func(_ *plumber.Plumber, tl *plumber.TaskList, _ *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					tl.SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})
				},
			}),
			Entry("plumber scope", commandRunnerScope{
				configure: func(app *plumber.Plumber, _ *plumber.TaskList, _ *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					app.SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})
				},
			}),
		)
	})

	It("should run plumber callbacks with scoped runtimes without mutating the plumber", func(ctx SpecContext) {
		defaultRunner := plumbertests.NewTestingCommandRunner()
		scopedRunner := plumbertests.NewTestingCommandRunner()
		fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: defaultRunner.Runner()})

		Expect(fixture.Plumber.RunWith(plumber.Runtime{CommandRunner: scopedRunner.Runner()}, func(app *plumber.Plumber) error {
			tl := plumber.NewTaskList(app)
			tl.Name = "list"
			tl.Set(func(tl *plumber.TaskList) plumber.Job {
				return tl.CreateTask("task").CreateCommand("scoped").Job()
			})

			return app.RunJobs(tl.Job())
		})).To(Succeed())
		Expect(scopedRunner.InvocationNames()).To(Equal([]string{"scoped"}))
		Expect(defaultRunner.Invocations()).To(BeEmpty())

		Expect(fixture.NewTaskList("list").CreateTask("task").CreateCommand("default").Run(ctx)).To(Succeed())
		Expect(defaultRunner.InvocationNames()).To(Equal([]string{"default"}))
	})

	DescribeTable("should run dynamically created commands with RunWith",
		func(ctx SpecContext, run func(context.Context, *plumbertests.PlumberFixture, plumber.Runtime) error, expected plumber.CommandInvocation) {
			runner := plumbertests.NewTestingCommandRunner()
			runtime := plumber.Runtime{CommandRunner: runner.Runner()}

			Expect(run(ctx, fixture, runtime)).To(Succeed())
			Expect(runner.Invocations()).To(HaveLen(1))
			Expect(runner.Invocations()[0].Name).To(Equal(expected.Name))
			Expect(runner.Invocations()[0].Args).To(Equal(expected.Args))
		},
		Entry("task", func(ctx context.Context, fixture *plumbertests.PlumberFixture, runtime plumber.Runtime) error {
			task := fixture.NewTaskList("list").CreateTask("task").
				Set(func(_ context.Context, t *plumber.Task) error {
					t.CreateCommand("mock", "arg").AddSelfToTheTask()

					return nil
				}).
				ShouldRunAfter(func(ctx context.Context, t *plumber.Task) error {
					return t.RunCommandJobAsJobSequence(ctx)
				})

			return task.RunWith(ctx, runtime)
		}, plumber.CommandInvocation{Name: "mock", Args: []string{"arg"}}),
		Entry("task list", func(ctx context.Context, fixture *plumbertests.PlumberFixture, runtime plumber.Runtime) error {
			tl := fixture.NewTaskList("list").
				Set(func(tl *plumber.TaskList) plumber.Job {
					return tl.CreateTask("task").
						Set(func(_ context.Context, t *plumber.Task) error {
							t.CreateCommand("mock").AddSelfToTheTask()

							return nil
						}).
						ShouldRunAfter(func(ctx context.Context, t *plumber.Task) error {
							return t.RunCommandJobAsJobSequence(ctx)
						}).
						Job()
				})

			return tl.RunWith(ctx, runtime)
		}, plumber.CommandInvocation{Name: "mock", Args: []string{}}),
	)

	Context("RunWith idempotence", func() {
		It("should keep command runners unchanged after scoped command failures", func(ctx SpecContext) {
			failure := plumbertests.TestingCommandFailure(1)
			defaultRunner := plumbertests.NewTestingCommandRunner()
			scopedRunner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Result: &failure})
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				SetRuntime(plumber.Runtime{CommandRunner: defaultRunner.Runner()})

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: scopedRunner.Runner()})).To(HaveOccurred())
			Expect(scopedRunner.Invocations()).To(HaveLen(1))
			Expect(defaultRunner.Invocations()).To(BeEmpty())

			Expect(command.Run(ctx)).To(Succeed())
			Expect(defaultRunner.Invocations()).To(HaveLen(1))
		})

		It("should keep task runners unchanged after scoped task failures", func(ctx SpecContext) {
			failure := plumbertests.TestingCommandFailure(1)
			defaultRunner := plumbertests.NewTestingCommandRunner()
			scopedRunner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Result: &failure})
			task := fixture.NewTaskList("list").CreateTask("task").
				SetRuntime(plumber.Runtime{CommandRunner: defaultRunner.Runner()}).
				Set(func(ctx context.Context, task *plumber.Task) error {
					return task.CreateCommand("mock").Run(ctx)
				})

			Expect(task.RunWith(ctx, plumber.Runtime{CommandRunner: scopedRunner.Runner()})).To(HaveOccurred())
			Expect(scopedRunner.Invocations()).To(HaveLen(1))
			Expect(defaultRunner.Invocations()).To(BeEmpty())

			Expect(task.Run(ctx)).To(Succeed())
			Expect(defaultRunner.Invocations()).To(HaveLen(1))
		})

		It("should keep task-list runners unchanged after scoped task-list failures", func(ctx SpecContext) {
			failure := plumbertests.TestingCommandFailure(1)
			defaultRunner := plumbertests.NewTestingCommandRunner()
			scopedRunner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Result: &failure})
			tl := fixture.NewTaskList("list").
				SetRuntime(plumber.Runtime{CommandRunner: defaultRunner.Runner()}).
				Set(func(tl *plumber.TaskList) plumber.Job {
					return tl.CreateTask("task").CreateCommand("mock").Job()
				})

			Expect(tl.RunWith(ctx, plumber.Runtime{CommandRunner: scopedRunner.Runner()})).To(HaveOccurred())
			Expect(scopedRunner.Invocations()).To(HaveLen(1))
			Expect(defaultRunner.Invocations()).To(BeEmpty())

			Expect(tl.CreateTask("restored").CreateCommand("mock").Run(ctx)).To(Succeed())
			Expect(defaultRunner.Invocations()).To(HaveLen(1))
		})

		It("should run prebuilt task commands with scoped runners without mutating the task runtime", func(ctx SpecContext) {
			defaultRunner := plumbertests.NewTestingCommandRunner()
			scopedRunner := plumbertests.NewTestingCommandRunner()
			task := fixture.NewTaskList("list").CreateTask("task").
				SetRuntime(plumber.Runtime{CommandRunner: defaultRunner.Runner()}).
				ShouldRunAfter(func(ctx context.Context, task *plumber.Task) error {
					return task.RunCommandJobAsJobSequence(ctx)
				})
			task.CreateCommand("mock").AddSelfToTheTask()

			Expect(task.RunWith(ctx, plumber.Runtime{CommandRunner: scopedRunner.Runner()})).To(Succeed())
			Expect(scopedRunner.InvocationNames()).To(Equal([]string{"mock"}))
			Expect(defaultRunner.Invocations()).To(BeEmpty())

			Expect(task.Run(ctx)).To(Succeed())
			Expect(defaultRunner.InvocationNames()).To(Equal([]string{"mock"}))
		})

	})

	Context("result handling", func() {
		It("should convert failed command results into retryable errors", func(ctx SpecContext) {
			result := plumbertests.TestingCommandFailure(9)
			runner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Result: &result}).
				Add(plumbertests.TestingCommandResponse{})
			retry := &plumber.CommandRetry{
				Tries: 1,
				Delay: time.Millisecond,
			}
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				SetRetries(retry)

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(Succeed())
			Expect(runner.Invocations()).To(HaveLen(2))
			Expect(retry.Tries).To(BeEquivalentTo(0))
		})

		It("should not retry command start errors", func(ctx SpecContext) {
			startErr := fmt.Errorf("start failed")
			startResult := plumber.CommandResult{}
			runner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{
					Result: &startResult,
					Err:    startErr,
				})
			retry := &plumber.CommandRetry{
				Tries: 1,
				Delay: time.Millisecond,
			}
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				SetRetries(retry)

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(MatchError(startErr))
			Expect(runner.Invocations()).To(HaveLen(1))
			Expect(retry.Tries).To(BeEquivalentTo(1))
			Expect(command.HasFailed()).To(BeFalse())
			Expect(command.HasExited()).To(BeFalse())
		})

		It("should expose mocked command status without process state", func(ctx SpecContext) {
			result := plumbertests.TestingCommandFailure(2)
			runner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Result: &result})
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				SetIgnoreError()

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(Succeed())
			Expect(command.HasFailed()).To(BeTrue())
			Expect(command.HasExited()).To(BeFalse())
		})
	})

	It("should pass command configuration to runner invocations", func(ctx SpecContext) {
		dir := plumbertests.TempDir()
		runner := plumbertests.NewTestingCommandRunner().
			Add(plumbertests.TestingCommandResponse{
				Name: "mock",
				Args: []string{"arg"},
				Match: func(invocation plumber.CommandInvocation) bool {
					return invocation.Name == "mock" &&
						invocation.Dir == dir &&
						invocation.Path == "/usr/bin/mock" &&
						invocation.EnsureIsAlive
				},
			})
		command := fixture.NewTaskList("list").CreateTask("task").
			CreateCommand("mock").
			AppendArgs("", "arg").
			SetDir(dir).
			SetPath("/usr/bin/mock").
			SetCredential(func(_ *plumber.Command, credential *syscall.Credential) *syscall.Credential {
				credential.Uid = 42
				credential.Gid = 43

				return credential
			}).
			EnsureIsAlive()

		Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(MatchError(ContainSubstring("Process not running anymore: $ mock arg")))
		invocation, ok := runner.LastInvocation()
		Expect(ok).To(BeTrue())
		Expect(invocation.Name).To(Equal("mock"))
		Expect(invocation.Args).To(Equal([]string{"arg"}))
		Expect(invocation.Dir).To(Equal(dir))
		Expect(invocation.Path).To(Equal("/usr/bin/mock"))
		Expect(invocation.EnsureIsAlive).To(BeTrue())
		Expect(invocation.SysProcAttr.Credential.Uid).To(BeEquivalentTo(42))
		Expect(invocation.SysProcAttr.Credential.Gid).To(BeEquivalentTo(43))
	})

	It("should preserve newline-terminated stream recording behavior", func(ctx SpecContext) {
		runner := plumbertests.NewTestingCommandRunner().
			Add(plumbertests.TestingCommandResponse{
				Stdout: "line\nunterminated",
			})
		command := fixture.NewTaskList("list").CreateTask("task").
			CreateCommand("mock").
			EnableStreamRecording()

		Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(Succeed())
		Expect(command.GetStdoutStream()).To(Equal([]string{"line\n"}))
	})

	It("should template file scripts into stdin before invoking the runner", func(ctx SpecContext) {
		dir := plumbertests.TempDir()
		path := filepath.Join(dir, "script.tmpl")
		Expect(os.WriteFile(path, []byte("hello {{ .Name }}\n"), 0600)).To(Succeed())
		runner := plumbertests.NewTestingCommandRunner()
		command := fixture.NewTaskList("list").CreateTask("task").
			CreateCommand("mock").
			SetScript(func(_ *plumber.Command) *plumber.CommandScript {
				return &plumber.CommandScript{
					File: path,
					Ctx: map[string]string{
						"Name": "file",
					},
				}
			})

		Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(Succeed())
		stdin, err := plumbertests.ReadInvocationStdin(runner.Invocations()[0])
		Expect(err).ToNot(HaveOccurred())
		Expect(stdin).To(Equal("hello file\n"))
	})

	Context("output capturing", func() {
		It("should capture the combined stream trimmed", func(ctx SpecContext) {
			var captured string
			runner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Stdout: "output\n", Stderr: "problem\n"})
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				CaptureOutput(&captured)

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(Succeed())
			Expect(captured).To(Equal("output\n\nproblem"))
		})

		It("should keep stdout and stderr captures on the same command separate", func(ctx SpecContext) {
			var stdout, stderr string
			runner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Stdout: "out\n", Stderr: "err\n"})
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				CaptureStdout(&stdout).
				CaptureStderr(&stderr)

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(Succeed())
			Expect(stdout).To(Equal("out"))
			Expect(stderr).To(Equal("err"))
		})

		It("should run captures before shouldRunAfterFn so it observes the captured value", func(ctx SpecContext) {
			var captured, observed string
			runner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Stdout: "output\n"})
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				CaptureOutput(&captured).
				ShouldRunAfter(func(_ context.Context, c *plumber.Command) error {
					observed = captured

					return nil
				})

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(Succeed())
			Expect(observed).To(Equal("output"))
		})

		It("should leave the destination untouched when the command fails without ignoring the error", func(ctx SpecContext) {
			captured := "unchanged"
			failure := plumbertests.TestingCommandFailure(1)
			runner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Stdout: "output\n", Result: &failure})
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				CaptureOutput(&captured)

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(HaveOccurred())
			Expect(captured).To(Equal("unchanged"))
		})

		It("should still capture output for a failing command with SetIgnoreError", func(ctx SpecContext) {
			var captured string
			failure := plumbertests.TestingCommandFailure(1)
			runner := plumbertests.NewTestingCommandRunner().
				Add(plumbertests.TestingCommandResponse{Stdout: "output\n", Result: &failure})
			command := fixture.NewTaskList("list").CreateTask("task").
				CreateCommand("mock").
				SetIgnoreError().
				CaptureOutput(&captured)

			Expect(command.RunWith(ctx, plumber.Runtime{CommandRunner: runner.Runner()})).To(Succeed())
			Expect(captured).To(Equal("output"))
		})

		It("should panic when the capture destination is nil", func() {
			command := fixture.NewTaskList("list").CreateTask("task").CreateCommand("mock")

			Expect(func() { command.CaptureOutput(nil) }).To(Panic())
		})
	})
})
