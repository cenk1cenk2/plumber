package plumber_test

import (
	"fmt"
	"sync"
	"time"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("floc helpers", func() {
	It("should compose predicates", func() {
		truthy := plumber.Predicate(func() bool {
			return true
		})
		falsey := plumber.Predicate(func() bool {
			return false
		})

		Expect(truthy(nil)).To(BeTrue())
		Expect(plumber.PredicateAnd(truthy, truthy)(nil)).To(BeTrue())
		Expect(plumber.PredicateOr(falsey, truthy)(nil)).To(BeTrue())
		Expect(plumber.PredicateNot(falsey)(nil)).To(BeTrue())
		Expect(plumber.PredicateXor(truthy, falsey)(nil)).To(BeTrue())
	})

	It("should run basic jobs in sequence, parallel, and repeat", func() {
		fixture := plumbertests.NewPlumber()
		var lock sync.Mutex
		order := []string{}
		appendOrder := func(value string) {
			lock.Lock()
			order = append(order, value)
			lock.Unlock()
		}

		Expect(fixture.Plumber.RunJobs(plumber.JobSequence(
			plumber.CreateJob(func() error {
				appendOrder("one")

				return nil
			}),
			plumber.JobParallel(
				plumber.CreateBasicJob(func() error {
					appendOrder("two")

					return nil
				}),
				plumber.CreateBasicJob(func() error {
					appendOrder("three")

					return nil
				}),
			),
			plumber.JobRepeat(
				plumber.CreateBasicJob(func() error {
					appendOrder("repeat")

					return nil
				}),
				2,
			),
		))).To(Succeed())
		Expect(order[0]).To(Equal("one"))
		Expect(order).To(ContainElements("two", "three", "repeat", "repeat"))
	})

	It("should branch and wait through helper jobs", func() {
		fixture := plumbertests.NewPlumber()
		order := []string{}
		ready := false

		Expect(fixture.Plumber.RunJobs(plumber.JobSequence(
			plumber.JobIf(
				plumber.Predicate(func() bool {
					return true
				}),
				plumber.JobThen(plumber.CreateBasicJob(func() error {
					order = append(order, "then")

					return nil
				})),
				plumber.JobElse(plumber.CreateBasicJob(func() error {
					order = append(order, "else")

					return nil
				})),
			),
			plumber.JobIfNot(
				plumber.Predicate(func() bool {
					return false
				}),
				plumber.CreateBasicJob(func() error {
					order = append(order, "if-not")

					return nil
				}),
			),
			plumber.JobDelay(plumber.CreateBasicJob(func() error {
				ready = true

				return nil
			}), time.Millisecond),
			plumber.JobWait(plumber.Predicate(func() bool {
				return ready
			}), time.Millisecond),
		))).To(Succeed())
		Expect(order).To(Equal([]string{"then", "if-not"}))
	})

	It("should guard panics and failed jobs", func() {
		fixture := plumbertests.NewPlumber()
		panicHandled := false

		Expect(fixture.Plumber.RunJobs(plumber.GuardIgnorePanic(plumber.CreateBasicJob(func() error {
			panic("ignored")
		})))).To(Succeed())
		Expect(fixture.Plumber.RunJobs(plumber.GuardOnPanic(plumber.CreateBasicJob(func() error {
			panic("handled")
		}), func() {
			panicHandled = true
		}))).To(Succeed())
		Expect(panicHandled).To(BeTrue())
		Expect(fixture.Plumber.RunJobs(plumber.GuardResume(plumber.CreateBasicJob(func() error {
			return fmt.Errorf("failed")
		}), plumber.TASK_FAILED))).To(Succeed())
		Expect(fixture.Plumber.RunJobs(plumber.GuardTimeout(plumber.CreateBasicJob(func() error {
			return nil
		}), time.Millisecond))).To(Succeed())
		Expect(plumber.NewJobResultMask(plumber.TASK_FAILED)).ToNot(BeNil())
	})
})
