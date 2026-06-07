package plumber_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type commandJobCase struct {
	prepare func(*plumber.Task, *plumbertests.TestingCommandRunner)
	run     func(*plumber.Task) error
	assert  func(*plumbertests.TestingCommandRunner)
}

type subtaskCase struct {
	prepare func(*plumber.Task, *[]string, *sync.Mutex)
	assert  func([]string)
}

type taskListStopCase struct {
	configure func(*plumber.TaskList)
	assert    func(*plumber.TaskList)
}

type taskLifecycleErrorCase struct {
	configure     func(*plumber.Task, *[]string)
	expectedError string
	expectedOrder []string
}

type taskListLifecycleErrorCase struct {
	configure     func(*plumber.TaskList, *[]string)
	run           func(*plumber.TaskList) error
	expectedError string
	expectedOrder []string
}

var _ = Describe("task behavior", func() {
	var fixture *plumbertests.PlumberFixture

	BeforeEach(func() {
		fixture = plumbertests.NewPlumber()
	})

	DescribeTable("should stop tasks before running hooks or body",
		func(configure func(*plumber.Task), expectedContext string) {
			task := fixture.NewTaskList("tasks").CreateTask(expectedContext)
			order := []string{}

			configure(task)
			task.
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
		},
		Entry("disabled task", func(task *plumber.Task) {
			task.ShouldDisable(func(_ *plumber.Task) bool {
				return true
			})
		}, "disabled"),
		Entry("skipped task", func(task *plumber.Task) {
			task.ShouldSkip(func(_ *plumber.Task) bool {
				return true
			})
		}, "skipped"),
	)

	DescribeTable("should return task lifecycle errors from the failing phase",
		func(tc taskLifecycleErrorCase) {
			fixture.Plumber.Log.ExitFunc = func(int) {}
			task := fixture.NewTaskList("tasks").CreateTask("failing")
			order := []string{}

			tc.configure(task, &order)

			Expect(task.Run()).To(MatchError(tc.expectedError))
			Expect(order).To(Equal(tc.expectedOrder))
		},
		Entry("before hook", taskLifecycleErrorCase{
			configure: func(task *plumber.Task, order *[]string) {
				task.
					ShouldRunBefore(func(_ *plumber.Task) error {
						*order = append(*order, "before")

						return errors.New("before failed")
					}).
					Set(func(_ *plumber.Task) error {
						*order = append(*order, "run")

						return nil
					}).
					ShouldRunAfter(func(_ *plumber.Task) error {
						*order = append(*order, "after")

						return nil
					})
			},
			expectedError: "before failed",
			expectedOrder: []string{"before"},
		}),
		Entry("body", taskLifecycleErrorCase{
			configure: func(task *plumber.Task, order *[]string) {
				task.
					ShouldRunBefore(func(_ *plumber.Task) error {
						*order = append(*order, "before")

						return nil
					}).
					Set(func(_ *plumber.Task) error {
						*order = append(*order, "run")

						return errors.New("run failed")
					}).
					ShouldRunAfter(func(_ *plumber.Task) error {
						*order = append(*order, "after")

						return nil
					})
			},
			expectedError: "run failed",
			expectedOrder: []string{"before", "run"},
		}),
		Entry("after hook", taskLifecycleErrorCase{
			configure: func(task *plumber.Task, order *[]string) {
				task.
					ShouldRunBefore(func(_ *plumber.Task) error {
						*order = append(*order, "before")

						return nil
					}).
					Set(func(_ *plumber.Task) error {
						*order = append(*order, "run")

						return nil
					}).
					ShouldRunAfter(func(_ *plumber.Task) error {
						*order = append(*order, "after")

						return errors.New("after failed")
					})
			},
			expectedError: "after failed",
			expectedOrder: []string{"before", "run", "after"},
		}),
	)

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

	It("should use scoped command runners while running a task", func() {
		defaultRunner := plumbertests.NewTestingCommandRunner()
		scopedRunner := plumbertests.NewTestingCommandRunner()
		task := fixture.NewTaskList("commands").CreateTask("task").
			SetRuntime(plumber.Runtime{CommandRunner: defaultRunner.Runner()}).
			Set(func(t *plumber.Task) error {
				return t.CreateCommand("scoped").Run()
			})

		Expect(task.RunWith(plumber.Runtime{CommandRunner: scopedRunner.Runner()})).To(Succeed())
		Expect(scopedRunner.InvocationNames()).To(Equal([]string{"scoped"}))
		Expect(defaultRunner.Invocations()).To(BeEmpty())

		Expect(task.Run()).To(Succeed())
		Expect(defaultRunner.InvocationNames()).To(Equal([]string{"scoped"}))
	})

	DescribeTable("should aggregate and run command jobs",
		func(tc commandJobCase) {
			runner := plumbertests.NewTestingCommandRunner()
			task := fixture.NewTaskList("commands").CreateTask("task").SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})

			tc.prepare(task, runner)

			Expect(tc.run(task)).To(Succeed())
			tc.assert(runner)
		},
		Entry("as a sequence", commandJobCase{
			prepare: func(task *plumber.Task, _ *plumbertests.TestingCommandRunner) {
				one := task.CreateCommand("one").AddSelfToTheTask()
				two := task.CreateCommand("two").AddSelfToTheTask()

				Expect(task.GetCommands()).To(Equal([]*plumber.Command{one, two}))
				Expect(task.GetCommandJobs()).To(HaveLen(2))
				Expect(task.GetCommandJobAsJobSequence()).ToNot(BeNil())
				Expect(task.GetCommandJobAsJobParallel()).ToNot(BeNil())
			},
			run: func(task *plumber.Task) error {
				return task.RunCommandJobAsJobSequence()
			},
			assert: func(runner *plumbertests.TestingCommandRunner) {
				Expect(runner.InvocationNames()).To(Equal([]string{"one", "two"}))
			},
		}),
		Entry("as parallel work", commandJobCase{
			prepare: func(task *plumber.Task, runner *plumbertests.TestingCommandRunner) {
				runner.AddResponses(
					plumbertests.TestingCommandResponse{Name: "one"},
					plumbertests.TestingCommandResponse{Name: "two"},
				)
				task.CreateCommand("one").AddSelfToTheTask()
				task.CreateCommand("two").AddSelfToTheTask()
			},
			run: func(task *plumber.Task) error {
				return task.RunCommandJobAsJobParallel()
			},
			assert: func(runner *plumbertests.TestingCommandRunner) {
				Expect(runner.InvocationNames()).To(ConsistOf("one", "two"))
			},
		}),
		Entry("with a custom parser", commandJobCase{
			prepare: func(task *plumber.Task, _ *plumbertests.TestingCommandRunner) {
				task.CreateCommand("mock").AddSelfToTheTask()
			},
			run: func(task *plumber.Task) error {
				return task.RunCommandJob(func(t *plumber.Task) plumber.Job {
					return t.GetCommandJobAsJobSequence()
				})
			},
			assert: func(runner *plumbertests.TestingCommandRunner) {
				Expect(runner.InvocationNames()).To(Equal([]string{"mock"}))
			},
		}),
	)

	It("should run command jobs through command wrappers", func() {
		runner := plumbertests.NewTestingCommandRunner()
		task := fixture.NewTaskList("commands").CreateTask("task").SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})
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
		command := child.CreateCommand("mock").SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()}).AddSelfToTheParentTask(parent)

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

	DescribeTable("should create and run subtasks",
		func(tc subtaskCase) {
			parent := fixture.NewTaskList("tasks").CreateTask("parent")
			var lock sync.Mutex
			order := []string{}

			tc.prepare(parent, &order, &lock)

			Expect(parent.RunSubtasks()).To(Succeed())
			tc.assert(order)
		},
		Entry("in sequence", subtaskCase{
			prepare: func(parent *plumber.Task, order *[]string, _ *sync.Mutex) {
				child := parent.CreateSubtask("child").
					Set(func(_ *plumber.Task) error {
						*order = append(*order, "child")

						return nil
					}).
					AddSelfToTheParentAsSequence()

				Expect(child.HasParent()).To(BeTrue())
				Expect(child.Name).To(Equal("parent:child"))
				Expect(parent.GetSubtasks()).ToNot(BeNil())
			},
			assert: func(order []string) {
				Expect(order).To(Equal([]string{"child"}))
			},
		}),
		Entry("in parallel", subtaskCase{
			prepare: func(parent *plumber.Task, order *[]string, lock *sync.Mutex) {
				for _, name := range []string{"one", "two"} {
					current := name
					parent.CreateSubtask(current).
						Set(func(_ *plumber.Task) error {
							lock.Lock()
							*order = append(*order, current)
							lock.Unlock()

							return nil
						}).
						AddSelfToTheParentAsParallel()
				}
			},
			assert: func(order []string) {
				Expect(order).To(ConsistOf("one", "two"))
			},
		}),
	)

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

	It("should attach subtasks to an arbitrary parent", func() {
		source := fixture.NewTaskList("tasks").CreateTask("source")
		target := fixture.NewTaskList("tasks").CreateTask("target")
		order := []string{}
		child := source.CreateSubtask("child").Set(func(_ *plumber.Task) error {
			order = append(order, "child")

			return nil
		})

		result := child.ToParent(target, func(parent *plumber.Task, child *plumber.Task) {
			parent.SetSubtask(child.Job())
		})

		Expect(result).To(BeIdenticalTo(child))
		Expect(target.RunSubtasks()).To(Succeed())
		Expect(order).To(Equal([]string{"child"}))
	})

	It("should extend subtask jobs with wrappers", func() {
		parent := fixture.NewTaskList("tasks").CreateTask("parent")
		order := []string{}

		parent.
			SetSubtask(plumber.CreateBasicJob(func() error {
				order = append(order, "base")

				return nil
			})).
			ExtendSubtask(func(job plumber.Job) plumber.Job {
				return plumber.JobSequence(job, plumber.CreateBasicJob(func() error {
					order = append(order, "extended")

					return nil
				}))
			})

		Expect(parent.RunSubtasks()).To(Succeed())
		Expect(order).To(Equal([]string{"base", "extended"}))
	})

	It("should reset nil subtasks to an empty job", func() {
		parent := fixture.NewTaskList("tasks").CreateTask("parent")

		parent.SetSubtask(nil)

		Expect(parent.RunSubtasks()).To(Succeed())
		Expect(parent.GetSubtasks()).ToNot(BeNil())
	})
})

var _ = Describe("task lists", func() {
	DescribeTable("should return task list lifecycle errors from the failing phase",
		func(tc taskListLifecycleErrorCase) {
			fixture := plumbertests.NewPlumber()
			tl := fixture.NewTaskList("failing")
			order := []string{}

			tc.configure(tl, &order)

			Expect(tc.run(tl)).To(MatchError(tc.expectedError))
			Expect(order).To(Equal(tc.expectedOrder))
		},
		Entry("before hook", taskListLifecycleErrorCase{
			configure: func(tl *plumber.TaskList, order *[]string) {
				tl.ShouldRunBefore(func(_ *plumber.TaskList) error {
					*order = append(*order, "before")

					return errors.New("before failed")
				})
			},
			run: func(tl *plumber.TaskList) error {
				return tl.RunBefore()
			},
			expectedError: "before failed",
			expectedOrder: []string{"before"},
		}),
		Entry("run job", taskListLifecycleErrorCase{
			configure: func(tl *plumber.TaskList, order *[]string) {
				tl.Set(func(_ *plumber.TaskList) plumber.Job {
					return plumber.CreateBasicJob(func() error {
						*order = append(*order, "run")

						return errors.New("run failed")
					})
				})
			},
			run: func(tl *plumber.TaskList) error {
				return tl.Run()
			},
			expectedError: "run failed",
			expectedOrder: []string{"run"},
		}),
		Entry("after hook", taskListLifecycleErrorCase{
			configure: func(tl *plumber.TaskList, order *[]string) {
				tl.ShouldRunAfter(func(_ *plumber.TaskList) error {
					*order = append(*order, "after")

					return errors.New("after failed")
				})
			},
			run: func(tl *plumber.TaskList) error {
				return tl.RunAfter()
			},
			expectedError: "after failed",
			expectedOrder: []string{"after"},
		}),
	)

	DescribeTable("should stop task list phases before running work",
		func(tc taskListStopCase) {
			fixture := plumbertests.NewPlumber()
			tl := fixture.NewTaskList("stopped").
				SetRuntimeDepth(2).
				Set(func(_ *plumber.TaskList) plumber.Job {
					return plumber.CreateBasicJob(func() error {
						return fmt.Errorf("should not run")
					})
				})

			tc.configure(tl)

			Expect(tl.RunBefore()).To(Succeed())
			Expect(tl.Run()).To(Succeed())
			Expect(tl.RunAfter()).To(Succeed())
			tc.assert(tl)
		},
		Entry("disabled", taskListStopCase{
			configure: func(tl *plumber.TaskList) {
				tl.ShouldDisable(func(_ *plumber.TaskList) bool {
					return true
				})
			},
			assert: func(tl *plumber.TaskList) {
				Expect(tl.IsDisabled()).To(BeTrue())
				Expect(tl.IsSkipped()).To(BeFalse())
			},
		}),
		Entry("skipped", taskListStopCase{
			configure: func(tl *plumber.TaskList) {
				tl.ShouldSkip(func(_ *plumber.TaskList) bool {
					return true
				})
			},
			assert: func(tl *plumber.TaskList) {
				Expect(tl.IsDisabled()).To(BeFalse())
				Expect(tl.IsSkipped()).To(BeTrue())
			},
		}),
		Entry("disabled and skipped", taskListStopCase{
			configure: func(tl *plumber.TaskList) {
				tl.ShouldDisable(func(_ *plumber.TaskList) bool {
					return true
				})
				tl.ShouldSkip(func(_ *plumber.TaskList) bool {
					return true
				})
			},
			assert: func(tl *plumber.TaskList) {
				Expect(tl.IsDisabled()).To(BeTrue())
				Expect(tl.IsSkipped()).To(BeTrue())
			},
		}),
	)

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

	It("should stop combined task lists before later run and after phases when a run phase fails", func() {
		fixture := plumbertests.NewPlumber()
		var lock sync.Mutex
		order := []string{}
		appendOrder := func(value string) {
			lock.Lock()
			order = append(order, value)
			lock.Unlock()
		}
		create := func(name string, err error) *plumber.TaskList {
			return fixture.NewTaskList(name).
				ShouldRunBefore(func(_ *plumber.TaskList) error {
					appendOrder(name + ":before")

					return nil
				}).
				Set(func(_ *plumber.TaskList) plumber.Job {
					return plumber.CreateBasicJob(func() error {
						appendOrder(name + ":run")

						return err
					})
				}).
				ShouldRunAfter(func(_ *plumber.TaskList) error {
					appendOrder(name + ":after")

					return nil
				})
		}

		err := fixture.Plumber.RunJobs(plumber.CombineTaskLists(
			create("one", errors.New("one failed")),
			create("two", nil),
		))

		Expect(err).To(MatchError("one failed"))
		Expect(order).To(ContainElements("one:before", "two:before", "one:run"))
		Expect(order).ToNot(ContainElements("two:run", "one:after", "two:after"))
	})

	It("should use scoped command runners while running a task list", func() {
		fixture := plumbertests.NewPlumber()
		defaultRunner := plumbertests.NewTestingCommandRunner()
		scopedRunner := plumbertests.NewTestingCommandRunner()
		tl := fixture.NewTaskList("commands").
			SetRuntime(plumber.Runtime{CommandRunner: defaultRunner.Runner()}).
			Set(func(tl *plumber.TaskList) plumber.Job {
				return tl.CreateTask("task").CreateCommand("scoped").Job()
			})

		Expect(tl.RunWith(plumber.Runtime{CommandRunner: scopedRunner.Runner()})).To(Succeed())
		Expect(scopedRunner.InvocationNames()).To(Equal([]string{"scoped"}))
		Expect(defaultRunner.Invocations()).To(BeEmpty())

		Expect(tl.Run()).To(Succeed())
		Expect(defaultRunner.InvocationNames()).To(Equal([]string{"scoped"}))
	})
})
