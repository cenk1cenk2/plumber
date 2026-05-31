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

type predicateCase struct {
	build func(plumber.JobPredicate, plumber.JobPredicate) plumber.JobPredicate
}

type guardCase struct {
	job    func(*bool) plumber.Job
	assert func(bool)
}

var _ = Describe("floc helpers", func() {
	DescribeTable("should compose predicates",
		func(tc predicateCase) {
			truthy := plumber.Predicate(func() bool {
				return true
			})
			falsey := plumber.Predicate(func() bool {
				return false
			})

			Expect(tc.build(truthy, falsey)(nil)).To(BeTrue())
		},
		Entry("simple predicate", predicateCase{
			build: func(truthy plumber.JobPredicate, _ plumber.JobPredicate) plumber.JobPredicate {
				return truthy
			},
		}),
		Entry("and", predicateCase{
			build: func(truthy plumber.JobPredicate, _ plumber.JobPredicate) plumber.JobPredicate {
				return plumber.PredicateAnd(truthy, truthy)
			},
		}),
		Entry("or", predicateCase{
			build: func(truthy plumber.JobPredicate, falsey plumber.JobPredicate) plumber.JobPredicate {
				return plumber.PredicateOr(falsey, truthy)
			},
		}),
		Entry("not", predicateCase{
			build: func(_ plumber.JobPredicate, falsey plumber.JobPredicate) plumber.JobPredicate {
				return plumber.PredicateNot(falsey)
			},
		}),
		Entry("xor", predicateCase{
			build: func(truthy plumber.JobPredicate, falsey plumber.JobPredicate) plumber.JobPredicate {
				return plumber.PredicateXor(truthy, falsey)
			},
		}),
	)

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

	DescribeTable("should guard jobs",
		func(tc guardCase) {
			fixture := plumbertests.NewPlumber()
			handled := false

			Expect(fixture.Plumber.RunJobs(tc.job(&handled))).To(Succeed())
			if tc.assert != nil {
				tc.assert(handled)
			}
		},
		Entry("ignore panic", guardCase{
			job: func(_ *bool) plumber.Job {
				return plumber.GuardIgnorePanic(plumber.CreateBasicJob(func() error {
					panic("ignored")
				}))
			},
		}),
		Entry("handle panic", guardCase{
			job: func(handled *bool) plumber.Job {
				return plumber.GuardOnPanic(plumber.CreateBasicJob(func() error {
					panic("handled")
				}), func() {
					*handled = true
				})
			},
			assert: func(handled bool) {
				Expect(handled).To(BeTrue())
			},
		}),
		Entry("resume failed job", guardCase{
			job: func(_ *bool) plumber.Job {
				return plumber.GuardResume(plumber.CreateBasicJob(func() error {
					return fmt.Errorf("failed")
				}), plumber.TASK_FAILED)
			},
		}),
		Entry("timeout successful job", guardCase{
			job: func(_ *bool) plumber.Job {
				return plumber.GuardTimeout(plumber.CreateBasicJob(func() error {
					return nil
				}), time.Millisecond)
			},
		}),
	)

	It("should create result masks", func() {
		Expect(plumber.NewJobResultMask(plumber.TASK_FAILED)).ToNot(BeNil())
	})
})
