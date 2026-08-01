package plumber_test

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type contextRunner struct {
	fn func(ctx context.Context, invocation plumber.CommandInvocation) (plumber.CommandResult, error)
}

func (r *contextRunner) Run(
	ctx context.Context,
	invocation plumber.CommandInvocation,
	_ plumber.CommandRuntime,
) (plumber.CommandResult, error) {
	if r.fn != nil {
		return r.fn(ctx, invocation)
	}

	return plumbertests.TestingCommandSuccess(), nil
}

type contextIsolationCase struct {
	run      func(*plumbertests.PlumberFixture)
	expected []error
}

type contextPropagationCase struct {
	run func(*plumbertests.PlumberFixture, *plumber.Task) error
}

var _ = Describe("floc context isolation", func() {
	DescribeTable("should not leak the cancellation of a flow to the flows around it",
		func(tc contextIsolationCase) {
			lock := &sync.Mutex{}
			errored := []error{}
			runner := &contextRunner{
				fn: func(ctx context.Context, _ plumber.CommandInvocation) (plumber.CommandResult, error) {
					lock.Lock()
					errored = append(errored, ctx.Err())
					lock.Unlock()

					return plumbertests.TestingCommandSuccess(), nil
				},
			}

			fixture := plumbertests.NewPlumber()
			fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: runner})

			tc.run(fixture)

			lock.Lock()
			defer lock.Unlock()

			Expect(errored).To(Equal(tc.expected))
		},
		Entry("a failed flow that came before", contextIsolationCase{
			run: func(fixture *plumbertests.PlumberFixture) {
				tl := fixture.NewTaskList("isolation")

				Expect(fixture.Plumber.RunJobs(plumber.JobSequence(plumber.CreateBasicJob(func() error {
					return errors.New("first flow failed")
				})))).To(MatchError("first flow failed"))

				Expect(tl.CreateTask("second").CreateCommand("second").Run()).To(Succeed())
			},
			expected: []error{nil},
		}),
		Entry("the task lists that came before", contextIsolationCase{
			run: func(fixture *plumbertests.PlumberFixture) {
				lists := []*plumber.TaskList{}
				for _, name := range []string{"first", "second", "third"} {
					tl := fixture.NewTaskList(name)
					tl.Set(func(tl *plumber.TaskList) plumber.Job {
						return tl.CreateTask(name).
							Set(func(t *plumber.Task) error {
								t.CreateCommand(name).AddSelfToTheTask()

								return t.RunCommandJobAsJobSequence()
							}).
							Job()
					})

					lists = append(lists, tl)
				}

				Expect(fixture.Plumber.RunJobs(plumber.CombineTaskLists(lists...))).To(Succeed())
			},
			expected: []error{nil, nil, nil},
		}),
		Entry("a nested flow that is over", contextIsolationCase{
			run: func(fixture *plumbertests.PlumberFixture) {
				t := fixture.NewTaskList("nested").CreateTask("nested")

				Expect(fixture.Plumber.RunJobs(plumber.JobSequence(
					plumber.CreateBasicJob(func() error {
						return fixture.Plumber.RunJobs(plumber.JobSequence(
							t.CreateCommand("nested").Job(),
						))
					}),
					t.CreateCommand("outer").Job(),
				))).To(Succeed())
			},
			expected: []error{nil, nil},
		}),
	)

	It("should not leak the cancellation of a flow to the flow that runs next to it", func(_ SpecContext) {
		lock := &sync.Mutex{}
		errored := []error{}
		started := make(chan bool)
		overlapped := make(chan bool)
		finished := make(chan bool)
		runner := &contextRunner{
			fn: func(ctx context.Context, invocation plumber.CommandInvocation) (plumber.CommandResult, error) {
				// Overlap the two nested flows and let the one that started first finish
				// first, so a flow that hands its own context to the one next to it leaves a
				// cancelled context behind for everything that comes after them.
				switch invocation.Name {
				case "first":
					close(started)
					<-overlapped
				case "second":
					close(overlapped)
					<-finished
				}

				lock.Lock()
				errored = append(errored, ctx.Err())
				lock.Unlock()

				return plumbertests.TestingCommandSuccess(), nil
			},
		}

		fixture := plumbertests.NewPlumber()
		fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: runner})

		t := fixture.NewTaskList("concurrent").CreateTask("concurrent")

		Expect(fixture.Plumber.RunJobs(plumber.JobSequence(
			plumber.JobParallel(
				plumber.CreateBasicJob(func() error {
					defer close(finished)

					return fixture.Plumber.RunJobs(plumber.JobSequence(
						t.CreateCommand("first").Job(),
					))
				}),
				plumber.CreateBasicJob(func() error {
					<-started

					return fixture.Plumber.RunJobs(plumber.JobSequence(
						t.CreateCommand("second").Job(),
					))
				}),
			),
			t.CreateCommand("third").Job(),
		))).To(Succeed())

		Expect(fixture.Plumber.RunJobs(plumber.JobSequence(
			t.CreateCommand("fourth").Job(),
		))).To(Succeed())

		lock.Lock()
		defer lock.Unlock()

		Expect(errored).To(Equal([]error{nil, nil, nil, nil}))
	}, SpecTimeout(time.Second*10))

	It("should not leak the cancellation of a flow to an unrelated flow that was started next to it", func(_ SpecContext) {
		lock := &sync.Mutex{}
		errored := []error{}
		running := make(chan bool)
		registered := make(chan bool)
		finished := make(chan bool)
		runner := &contextRunner{
			fn: func(ctx context.Context, invocation plumber.CommandInvocation) (plumber.CommandResult, error) {
				// Let the second flow start while the first one is still running and check its
				// context only after the first one is over, so a flow that hangs itself on
				// whichever flow runs at the moment gets cancelled by an unrelated one.
				switch invocation.Name {
				case "first":
					close(running)
					<-registered
				case "second":
					close(registered)
					<-finished
				}

				lock.Lock()
				errored = append(errored, ctx.Err())
				lock.Unlock()

				return plumbertests.TestingCommandSuccess(), nil
			},
		}

		fixture := plumbertests.NewPlumber()
		fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: runner})

		t := fixture.NewTaskList("unrelated").CreateTask("unrelated")
		first := t.CreateCommand("first").Job()
		second := t.CreateCommand("second").Job()

		wg := &sync.WaitGroup{}
		wg.Add(2)

		go func() {
			defer GinkgoRecover()
			defer wg.Done()
			defer close(finished)

			Expect(fixture.Plumber.RunJobs(plumber.JobSequence(first))).To(Succeed())
		}()

		go func() {
			defer GinkgoRecover()
			defer wg.Done()

			<-running

			Expect(fixture.Plumber.RunJobs(plumber.JobSequence(second))).To(Succeed())
		}()

		wg.Wait()

		lock.Lock()
		defer lock.Unlock()

		Expect(errored).To(Equal([]error{nil, nil}))
	}, SpecTimeout(time.Second*10))

	DescribeTable("should cancel the running commands when the flow around them is cancelled",
		func(_ SpecContext, tc contextPropagationCase) {
			cancelled := make(chan error, 1)
			runner := &contextRunner{
				fn: func(ctx context.Context, invocation plumber.CommandInvocation) (plumber.CommandResult, error) {
					if invocation.Name == "dies" {
						return plumbertests.TestingCommandFailure(1), errors.New("supervised command exited")
					}

					select {
					case <-ctx.Done():
						cancelled <- ctx.Err()
					case <-time.After(time.Second * 5):
						cancelled <- nil
					}

					return plumbertests.TestingCommandSuccess(), nil
				},
			}

			fixture := plumbertests.NewPlumber()
			fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: runner})

			t := fixture.NewTaskList("supervised").CreateTask("daemons")

			Expect(tc.run(fixture, t)).To(HaveOccurred())

			Expect(<-cancelled).To(MatchError(context.Canceled))
		},
		Entry("a nested flow", contextPropagationCase{
			run: func(fixture *plumbertests.PlumberFixture, t *plumber.Task) error {
				return fixture.Plumber.RunJobs(plumber.JobParallel(
					plumber.CreateJobWithContext(func(ctx plumber.JobContext) error {
						return fixture.Plumber.RunJobsWith(ctx, plumber.JobSequence(
							t.CreateCommand("survives").Job(),
						))
					}),
					t.CreateCommand("dies").Job(),
				))
			},
		}, SpecTimeout(time.Second*10)),
		Entry("a sibling of the same flow", contextPropagationCase{
			run: func(fixture *plumbertests.PlumberFixture, t *plumber.Task) error {
				t.CreateCommand("survives").AddSelfToTheTask()
				t.CreateCommand("dies").AddSelfToTheTask()

				return fixture.Plumber.RunJobs(t.GetCommandJobAsJobParallel())
			},
		}, SpecTimeout(time.Second*10)),
	)
})
