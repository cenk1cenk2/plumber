package plumber_test

import (
	"fmt"
	"slices"
	"sync"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("task behavior", func() {
	var fixture *plumbertests.PlumberFixture

	BeforeEach(func() {
		fixture = plumbertests.NewPlumber()
	})

	It("should run skipped tasks without hooks or body", func() {
		task := fixture.NewTaskList("tasks").CreateTask("skipped")
		order := []string{}

		task.
			ShouldSkip(func(_ *plumber.Task) bool {
				return true
			}).
			ShouldRunBefore(func(_ *plumber.Task) error {
				order = append(order, "before")

				return nil
			}).
			Set(func(_ *plumber.Task) error {
				order = append(order, "run")

				return nil
			})

		Expect(task.Run()).To(Succeed())
		Expect(order).To(BeEmpty())
	})

	It("should run task jobs through wrappers", func() {
		task := fixture.NewTaskList("tasks").CreateTask("wrapped")
		order := []string{}

		task.
			Set(func(_ *plumber.Task) error {
				order = append(order, "run")

				return nil
			}).
			SetJobWrapper(func(job plumber.Job, t *plumber.Task) plumber.Job {
				return plumber.CreateBasicJob(func() error {
					order = append(order, fmt.Sprintf("wrapper:%s", t.Name))

					return t.Plumber.RunJobs(job)
				})
			})

		Expect(fixture.Plumber.RunJobs(task.Job())).To(Succeed())
		Expect(order).To(Equal([]string{"wrapper:wrapped", "run"}))
	})

	It("should aggregate and run command jobs as a sequence", func() {
		runner := plumbertests.NewTestingCommandRunner()
		task := fixture.NewTaskList("commands").CreateTask("task").SetCommandRunner(runner.Runner())
		one := task.CreateCommand("one").AddSelfToTheTask()
		two := task.CreateCommand("two").AddSelfToTheTask()

		Expect(task.GetCommands()).To(Equal([]*plumber.Command{one, two}))
		Expect(task.GetCommandJobs()).To(HaveLen(2))
		Expect(task.GetCommandJobAsJobSequence()).ToNot(BeNil())
		Expect(task.GetCommandJobAsJobParallel()).ToNot(BeNil())
		Expect(task.RunCommandJobAsJobSequence()).To(Succeed())
		Expect(runner.Invocations()).To(HaveLen(2))
		Expect(runner.Invocations()[0].Name).To(Equal("one"))
		Expect(runner.Invocations()[1].Name).To(Equal("two"))
	})

	It("should aggregate and run command jobs as parallel work", func() {
		runner := plumbertests.NewTestingCommandRunner().
			Add(plumbertests.TestingCommandResponse{Name: "one"}).
			Add(plumbertests.TestingCommandResponse{Name: "two"})
		task := fixture.NewTaskList("commands").CreateTask("task").SetCommandRunner(runner.Runner())

		task.CreateCommand("one").AddSelfToTheTask()
		task.CreateCommand("two").AddSelfToTheTask()

		Expect(task.RunCommandJobAsJobParallel()).To(Succeed())

		names := []string{}
		for _, invocation := range runner.Invocations() {
			names = append(names, invocation.Name)
		}
		Expect(names).To(ConsistOf("one", "two"))
	})

	It("should run command jobs with a custom parser", func() {
		runner := plumbertests.NewTestingCommandRunner()
		task := fixture.NewTaskList("commands").CreateTask("task").SetCommandRunner(runner.Runner())
		task.CreateCommand("mock").AddSelfToTheTask()

		Expect(task.RunCommandJob(func(t *plumber.Task) plumber.Job {
			return t.GetCommandJobAsJobSequence()
		})).To(Succeed())
		Expect(runner.Invocations()).To(HaveLen(1))
	})

	It("should run command jobs through command wrappers", func() {
		runner := plumbertests.NewTestingCommandRunner()
		task := fixture.NewTaskList("commands").CreateTask("task").SetCommandRunner(runner.Runner())
		order := []string{}
		command := task.CreateCommand("mock").
			SetJobWrapper(func(job plumber.Job, c *plumber.Command) plumber.Job {
				return plumber.CreateBasicJob(func() error {
					order = append(order, c.GetFormattedCommand())

					return c.Plumber.RunJobs(job)
				})
			})

		Expect(fixture.Plumber.RunJobs(command.Job())).To(Succeed())
		Expect(order).To(Equal([]string{"$ mock"}))
		Expect(runner.Invocations()).To(HaveLen(1))
	})

	It("should add commands to another task", func() {
		runner := plumbertests.NewTestingCommandRunner()
		parent := fixture.NewTaskList("commands").CreateTask("parent")
		child := fixture.NewTaskList("commands").CreateTask("child")
		command := child.CreateCommand("mock").SetRunner(runner.Runner()).AddSelfToTheParentTask(parent)

		Expect(parent.GetCommands()).To(Equal([]*plumber.Command{command}))
		Expect(parent.RunCommandJobAsJobSequence()).To(Succeed())
		Expect(runner.Invocations()).To(HaveLen(1))
	})
})

var _ = Describe("subtasks", func() {
	var fixture *plumbertests.PlumberFixture

	BeforeEach(func() {
		fixture = plumbertests.NewPlumber()
	})

	It("should create and run subtasks in sequence", func() {
		parent := fixture.NewTaskList("tasks").CreateTask("parent")
		order := []string{}
		child := parent.CreateSubtask("child").
			Set(func(_ *plumber.Task) error {
				order = append(order, "child")

				return nil
			}).
			AddSelfToTheParentAsSequence()

		Expect(child.HasParent()).To(BeTrue())
		Expect(child.Name).To(Equal("parent:child"))
		Expect(parent.GetSubtasks()).ToNot(BeNil())
		Expect(parent.RunSubtasks()).To(Succeed())
		Expect(order).To(Equal([]string{"child"}))
	})

	It("should create and run subtasks in parallel", func() {
		parent := fixture.NewTaskList("tasks").CreateTask("parent")
		var lock sync.Mutex
		order := []string{}

		parent.CreateSubtask("one").
			Set(func(_ *plumber.Task) error {
				lock.Lock()
				order = append(order, "one")
				lock.Unlock()

				return nil
			}).
			AddSelfToTheParentAsParallel()
		parent.CreateSubtask("two").
			Set(func(_ *plumber.Task) error {
				lock.Lock()
				order = append(order, "two")
				lock.Unlock()

				return nil
			}).
			AddSelfToTheParentAsParallel()

		Expect(parent.RunSubtasks()).To(Succeed())
		Expect(order).To(ConsistOf("one", "two"))
	})

	It("should attach subtasks with a custom parent wrapper", func() {
		parent := fixture.NewTaskList("tasks").CreateTask("parent")
		child := parent.CreateSubtask("child").Set(func(_ *plumber.Task) error {
			return nil
		})

		child.AddSelfToTheParent(func(parent *plumber.Task, child *plumber.Task) {
			parent.SetSubtask(child.Job())
		})

		Expect(parent.RunSubtasks()).To(Succeed())
	})

	It("should reset nil subtasks to an empty job", func() {
		parent := fixture.NewTaskList("tasks").CreateTask("parent")

		parent.SetSubtask(nil)

		Expect(parent.RunSubtasks()).To(Succeed())
		Expect(parent.GetSubtasks()).ToNot(BeNil())
	})
})

var _ = Describe("task lists", func() {
	It("should skip disabled task list phases", func() {
		fixture := plumbertests.NewPlumber()
		tl := fixture.NewTaskList("disabled").
			ShouldDisable(func(_ *plumber.TaskList) bool {
				return true
			}).
			ShouldSkip(func(_ *plumber.TaskList) bool {
				return true
			}).
			SetRuntimeDepth(2).
			Set(func(_ *plumber.TaskList) plumber.Job {
				return plumber.CreateBasicJob(func() error {
					return fmt.Errorf("should not run")
				})
			})

		Expect(tl.IsDisabled()).To(BeTrue())
		Expect(tl.IsSkipped()).To(BeTrue())
		Expect(tl.RunBefore()).To(Succeed())
		Expect(tl.Run()).To(Succeed())
		Expect(tl.RunAfter()).To(Succeed())
	})

	It("should combine task lists with before, run, and after phases", func() {
		fixture := plumbertests.NewPlumber()
		var lock sync.Mutex
		order := []string{}
		appendOrder := func(value string) {
			lock.Lock()
			order = append(order, value)
			lock.Unlock()
		}
		create := func(name string) *plumber.TaskList {
			return fixture.NewTaskList(name).
				ShouldRunBefore(func(_ *plumber.TaskList) error {
					appendOrder(name + ":before")

					return nil
				}).
				Set(func(_ *plumber.TaskList) plumber.Job {
					return plumber.CreateBasicJob(func() error {
						appendOrder(name + ":run")

						return nil
					})
				}).
				ShouldRunAfter(func(_ *plumber.TaskList) error {
					appendOrder(name + ":after")

					return nil
				})
		}

		Expect(fixture.Plumber.RunJobs(plumber.CombineTaskLists(create("one"), create("two")))).To(Succeed())
		Expect(order).To(ContainElements("one:before", "two:before", "one:after", "two:after"))
		Expect(slices.Index(order, "one:run")).To(BeNumerically("<", slices.Index(order, "two:run")))
	})
})
