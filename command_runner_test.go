package plumber_test

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("command runners", func() {
	var fixture *plumbertests.PlumberFixture

	BeforeEach(func() {
		fixture = plumbertests.NewPlumber()
	})

	It("should resolve runners from the nearest configured command scope", func() {
		plumberRunner := plumbertests.NewTestingCommandRunner()
		taskListRunner := plumbertests.NewTestingCommandRunner()
		taskRunner := plumbertests.NewTestingCommandRunner()
		commandRunner := plumbertests.NewTestingCommandRunner()

		fixture.Plumber.SetCommandRunner(plumberRunner.Runner())
		tl := fixture.NewTaskList("list").SetCommandRunner(taskListRunner.Runner())
		task := tl.CreateTask("task").SetCommandRunner(taskRunner.Runner())
		command := task.CreateCommand("mock").SetRunner(commandRunner.Runner())

		Expect(command.Run()).To(Succeed())
		Expect(commandRunner.Invocations()).To(HaveLen(1))
		Expect(taskRunner.Invocations()).To(BeEmpty())
		Expect(taskListRunner.Invocations()).To(BeEmpty())
		Expect(plumberRunner.Invocations()).To(BeEmpty())
	})

	It("should resolve runners from task, task list, and plumber scopes", func() {
		cases := []struct {
			name      string
			configure func(*plumber.Plumber, *plumber.TaskList, *plumber.Task, *plumbertests.TestingCommandRunner)
			invokedBy func(*plumbertests.TestingCommandRunner) []plumber.CommandInvocation
		}{
			{
				name: "task",
				configure: func(_ *plumber.Plumber, _ *plumber.TaskList, task *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					task.SetCommandRunner(runner.Runner())
				},
				invokedBy: func(runner *plumbertests.TestingCommandRunner) []plumber.CommandInvocation {
					return runner.Invocations()
				},
			},
			{
				name: "task list",
				configure: func(_ *plumber.Plumber, tl *plumber.TaskList, _ *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					tl.SetCommandRunner(runner.Runner())
				},
				invokedBy: func(runner *plumbertests.TestingCommandRunner) []plumber.CommandInvocation {
					return runner.Invocations()
				},
			},
			{
				name: "plumber",
				configure: func(app *plumber.Plumber, _ *plumber.TaskList, _ *plumber.Task, runner *plumbertests.TestingCommandRunner) {
					app.SetCommandRunner(runner.Runner())
				},
				invokedBy: func(runner *plumbertests.TestingCommandRunner) []plumber.CommandInvocation {
					return runner.Invocations()
				},
			},
		}

		for _, tc := range cases {
			By(tc.name)

			currentFixture := plumbertests.NewPlumber()
			runner := plumbertests.NewTestingCommandRunner()
			tl := currentFixture.NewTaskList("list")
			task := tl.CreateTask("task")

			tc.configure(currentFixture.Plumber, tl, task, runner)

			Expect(task.CreateCommand("mock").Run()).To(Succeed())
			Expect(tc.invokedBy(runner)).To(HaveLen(1))
			Expect(runner.Invocations()[0].TaskName).To(Equal("task"))
			Expect(runner.Invocations()[0].TaskListName).To(Equal("list"))
			Expect(runner.Invocations()[0].PlumberName).To(Equal("plumber-test"))
		}
	})

	It("should run dynamically created task commands with RunWith", func() {
		runner := plumbertests.NewTestingCommandRunner()
		task := fixture.NewTaskList("list").CreateTask("task").
			Set(func(t *plumber.Task) error {
				t.CreateCommand("mock", "arg").AddSelfToTheTask()

				return nil
			}).
			ShouldRunAfter(func(t *plumber.Task) error {
				return t.RunCommandJobAsJobSequence()
			})

		Expect(task.RunWith(runner.Runner())).To(Succeed())
		Expect(runner.Invocations()).To(HaveLen(1))
		Expect(runner.Invocations()[0].Name).To(Equal("mock"))
		Expect(runner.Invocations()[0].Args).To(Equal([]string{"arg"}))
	})

	It("should run dynamically created task-list commands with RunWith", func() {
		runner := plumbertests.NewTestingCommandRunner()
		tl := fixture.NewTaskList("list").
			Set(func(tl *plumber.TaskList) plumber.Job {
				return tl.CreateTask("task").
					Set(func(t *plumber.Task) error {
						t.CreateCommand("mock").AddSelfToTheTask()

						return nil
					}).
					ShouldRunAfter(func(t *plumber.Task) error {
						return t.RunCommandJobAsJobSequence()
					}).
					Job()
			})

		Expect(tl.RunWith(runner.Runner())).To(Succeed())
		Expect(runner.Invocations()).To(HaveLen(1))
	})

	It("should convert failed command results into retryable errors", func() {
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

		Expect(command.RunWith(runner.Runner())).To(Succeed())
		Expect(runner.Invocations()).To(HaveLen(2))
		Expect(retry.Tries).To(BeEquivalentTo(0))
	})

	It("should not retry command start errors", func() {
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

		Expect(command.RunWith(runner.Runner())).To(MatchError(startErr))
		Expect(runner.Invocations()).To(HaveLen(1))
		Expect(retry.Tries).To(BeEquivalentTo(1))
		Expect(command.HasFailed()).To(BeFalse())
		Expect(command.HasExited()).To(BeFalse())
	})

	It("should expose mocked command status without process state", func() {
		result := plumbertests.TestingCommandFailure(2)
		runner := plumbertests.NewTestingCommandRunner().
			Add(plumbertests.TestingCommandResponse{Result: &result})
		command := fixture.NewTaskList("list").CreateTask("task").
			CreateCommand("mock").
			SetIgnoreError()

		Expect(command.RunWith(runner.Runner())).To(Succeed())
		Expect(command.HasFailed()).To(BeTrue())
		Expect(command.HasExited()).To(BeFalse())
	})

	It("should pass command configuration to runner invocations", func() {
		dir := plumbertests.TempDir()
		runner := plumbertests.NewTestingCommandRunner()
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

		Expect(command.RunWith(runner.Runner())).To(MatchError(ContainSubstring("Process not running anymore: $ mock arg")))
		Expect(runner.Invocations()).To(HaveLen(1))
		Expect(runner.Invocations()[0].Name).To(Equal("mock"))
		Expect(runner.Invocations()[0].Args).To(Equal([]string{"arg"}))
		Expect(runner.Invocations()[0].Dir).To(Equal(dir))
		Expect(runner.Invocations()[0].Path).To(Equal("/usr/bin/mock"))
		Expect(runner.Invocations()[0].EnsureIsAlive).To(BeTrue())
		Expect(runner.Invocations()[0].SysProcAttr.Credential.Uid).To(BeEquivalentTo(42))
		Expect(runner.Invocations()[0].SysProcAttr.Credential.Gid).To(BeEquivalentTo(43))
	})

	It("should preserve newline-terminated stream recording behavior", func() {
		runner := plumbertests.NewTestingCommandRunner().
			Add(plumbertests.TestingCommandResponse{
				Stdout: "line\nunterminated",
			})
		command := fixture.NewTaskList("list").CreateTask("task").
			CreateCommand("mock").
			EnableStreamRecording()

		Expect(command.RunWith(runner.Runner())).To(Succeed())
		Expect(command.GetStdoutStream()).To(Equal([]string{"line\n"}))
	})

	It("should template file scripts into stdin before invoking the runner", func() {
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

		Expect(command.RunWith(runner.Runner())).To(Succeed())
		stdin, err := plumbertests.ReadInvocationStdin(runner.Invocations()[0])
		Expect(err).ToNot(HaveOccurred())
		Expect(stdin).To(Equal("hello file\n"))
	})
})
