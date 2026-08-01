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

var _ = Describe("floc context isolation", func() {
	It("should not leak the cancellation of a failed flow to the next one", func() {
		errored := []error{}
		runner := &contextRunner{
			fn: func(ctx context.Context, _ plumber.CommandInvocation) (plumber.CommandResult, error) {
				errored = append(errored, ctx.Err())

				return plumbertests.TestingCommandSuccess(), nil
			},
		}

		fixture := plumbertests.NewPlumber()
		fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: runner})

		tl := fixture.NewTaskList("isolation")

		Expect(fixture.Plumber.RunJobs(plumber.JobSequence(plumber.CreateBasicJob(func() error {
			return errors.New("first flow failed")
		})))).To(MatchError("first flow failed"))

		Expect(tl.CreateTask("second").CreateCommand("second").Run()).To(Succeed())

		Expect(errored).To(Equal([]error{nil}))
	})

	It("should keep the context alive between combined task lists", func() {
		errored := []error{}
		lock := &sync.Mutex{}
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

		Expect(errored).To(Equal([]error{nil, nil, nil}))
	})

	It("should not leak the cancellation of a nested flow to the flow around it", func() {
		errored := []error{}
		runner := &contextRunner{
			fn: func(ctx context.Context, _ plumber.CommandInvocation) (plumber.CommandResult, error) {
				errored = append(errored, ctx.Err())

				return plumbertests.TestingCommandSuccess(), nil
			},
		}

		fixture := plumbertests.NewPlumber()
		fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: runner})

		tl := fixture.NewTaskList("nested")
		t := tl.CreateTask("nested")

		Expect(fixture.Plumber.RunJobs(plumber.JobSequence(
			plumber.CreateBasicJob(func() error {
				return fixture.Plumber.RunJobs(plumber.JobSequence(
					t.CreateCommand("nested").Job(),
				))
			}),
			t.CreateCommand("outer").Job(),
		))).To(Succeed())

		Expect(errored).To(Equal([]error{nil, nil}))
	})

	It("should cancel a nested flow when the flow around it is cancelled", func() {
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

		tl := fixture.NewTaskList("supervised")
		t := tl.CreateTask("daemons")

		Expect(fixture.Plumber.RunJobs(plumber.JobParallel(
			plumber.CreateBasicJob(func() error {
				return fixture.Plumber.RunJobs(plumber.JobSequence(
					t.CreateCommand("survives").Job(),
				))
			}),
			t.CreateCommand("dies").Job(),
		))).To(HaveOccurred())

		Expect(<-cancelled).To(MatchError(context.Canceled))
	})

	It("should cancel the running commands when a sibling fails", func() {
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

		tl := fixture.NewTaskList("supervised")
		t := tl.CreateTask("daemons")
		t.CreateCommand("survives").AddSelfToTheTask()
		t.CreateCommand("dies").AddSelfToTheTask()

		Expect(fixture.Plumber.RunJobs(t.GetCommandJobAsJobParallel())).To(HaveOccurred())

		Expect(<-cancelled).To(MatchError(context.Canceled))
	})
})
