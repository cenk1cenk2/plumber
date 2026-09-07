package plumber_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenk1cenk2/plumber/v7"
	plumbertests "github.com/cenk1cenk2/plumber/v7/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type predicateCase struct {
	build func(plumber.Predicate, plumber.Predicate) plumber.Predicate
}

var _ = Describe("jobs", func() {

	DescribeTable("should compose predicates",
		func(tc predicateCase) {
			truthy := plumber.Predicate(func() bool {
				return true
			})
			falsey := plumber.Predicate(func() bool {
				return false
			})

			Expect(tc.build(truthy, falsey)()).To(BeTrue())
		},
		Entry("simple predicate", predicateCase{
			build: func(truthy plumber.Predicate, _ plumber.Predicate) plumber.Predicate {
				return truthy
			},
		}),
		Entry("and", predicateCase{
			build: func(truthy plumber.Predicate, _ plumber.Predicate) plumber.Predicate {
				return plumber.PredicateAnd(truthy, truthy)
			},
		}),
		Entry("or", predicateCase{
			build: func(truthy plumber.Predicate, falsey plumber.Predicate) plumber.Predicate {
				return plumber.PredicateOr(falsey, truthy)
			},
		}),
		Entry("not", predicateCase{
			build: func(_ plumber.Predicate, falsey plumber.Predicate) plumber.Predicate {
				return plumber.PredicateNot(falsey)
			},
		}),
		Entry("xor", predicateCase{
			build: func(truthy plumber.Predicate, falsey plumber.Predicate) plumber.Predicate {
				return plumber.PredicateXor(truthy, falsey)
			},
		}),
	)

	It("should run basic jobs in sequence, parallel, and repeat", func(ctx SpecContext) {
		var lock sync.Mutex
		order := []string{}
		appendOrder := func(value string) {
			lock.Lock()
			order = append(order, value)
			lock.Unlock()
		}

		Expect(plumber.JobSequence(
			plumber.CreateJob(func() error {
				appendOrder("one")

				return nil
			}),
			plumber.JobParallel(
				plumber.CreateJob(func() error {
					appendOrder("two")

					return nil
				}),
				plumber.CreateJob(func() error {
					appendOrder("three")

					return nil
				}),
			),
			plumber.JobRepeat(
				plumber.CreateJob(func() error {
					appendOrder("repeat")

					return nil
				}),
				2,
			),
		)(ctx)).To(Succeed())
		Expect(order[0]).To(Equal("one"))
		Expect(order).To(ContainElements("two", "three", "repeat", "repeat"))
	})

	It("should branch and wait through helper jobs", func(ctx SpecContext) {
		order := []string{}
		ready := false

		Expect(plumber.JobSequence(
			plumber.JobIf(
				func() bool {
					return true
				},
				plumber.CreateJob(func() error {
					order = append(order, "then")

					return nil
				}),
				plumber.CreateJob(func() error {
					order = append(order, "else")

					return nil
				}),
			),
			plumber.JobIfNot(
				func() bool {
					return false
				},
				plumber.CreateJob(func() error {
					order = append(order, "if-not")

					return nil
				}),
			),
			plumber.JobDelay(plumber.CreateJob(func() error {
				ready = true

				return nil
			}), time.Millisecond),
			plumber.JobWait(func() bool {
				return ready
			}, time.Millisecond),
			plumber.CreateEmptyJob(),
		)(ctx)).To(Succeed())
		Expect(order).To(Equal([]string{"then", "if-not"}))
	})

	It("should panic when the conditional job is not given a job", func() {
		Expect(func() {
			plumber.JobIf(func() bool {
				return true
			})
		}).To(PanicWith(MatchError("Conditional job requires a job and optionally its alternative.")))

		Expect(func() {
			plumber.JobIf(
				func() bool {
					return true
				},
				plumber.CreateEmptyJob(),
				plumber.CreateEmptyJob(),
				plumber.CreateEmptyJob(),
			)
		}).To(PanicWith(MatchError("Conditional job requires a job and optionally its alternative.")))
	})

	Describe("sequence", func() {
		It("should stop on the first error", func(ctx SpecContext) {
			ran := false

			err := plumber.JobSequence(
				plumber.CreateJob(func() error {
					return fmt.Errorf("failed")
				}),
				plumber.CreateJob(func() error {
					ran = true

					return nil
				}),
			)(ctx)

			Expect(err).To(MatchError("failed"))
			Expect(ran).To(BeFalse())
		})

		It("should not run any job while the flow is cancelled", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			ran := false

			err := plumber.JobSequence(plumber.CreateJob(func() error {
				ran = true

				return nil
			}))(cancelled)

			Expect(err).To(MatchError(context.Canceled))
			Expect(ran).To(BeFalse())
		})
	})

	Describe("parallel", func() {
		It("should return the first real error and cancel its siblings", func(ctx SpecContext) {
			cancelled := make(chan struct{})

			err := plumber.JobParallel(
				func(_ context.Context) error {
					return fmt.Errorf("failed")
				},
				func(ctx context.Context) error {
					<-ctx.Done()

					close(cancelled)

					return fmt.Errorf("killed the process")
				},
			)(ctx)

			Expect(err).To(MatchError("failed"))
			Expect(cancelled).To(BeClosed())
		})

		It("should return the error of the flow when everything is cancelled", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)

			err := plumber.JobParallel(
				func(ctx context.Context) error {
					cancel()

					<-ctx.Done()

					return fmt.Errorf("killed the process")
				},
			)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
		})

		It("should wait for every job to finish", func(ctx SpecContext) {
			var finished atomic.Int32

			Expect(plumber.JobParallel(
				plumber.JobDelay(plumber.CreateJob(func() error {
					finished.Add(1)

					return nil
				}), time.Millisecond),
				plumber.JobDelay(plumber.CreateJob(func() error {
					finished.Add(1)

					return nil
				}), 5*time.Millisecond),
			)(ctx)).To(Succeed())

			Expect(finished.Load()).To(Equal(int32(2)))
		})
	})

	Describe("loops", func() {
		It("should stop looping when the job fails", func(ctx SpecContext) {
			count := 0

			err := plumber.JobLoop(plumber.CreateJob(func() error {
				count++

				if count == 3 {
					return fmt.Errorf("failed")
				}

				return nil
			}))(ctx)

			Expect(err).To(MatchError("failed"))
			Expect(count).To(Equal(3))
		})

		It("should stop looping when the flow is cancelled", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)
			defer cancel()

			var count atomic.Int32

			err := plumber.JobLoopWithWaitAfter(plumber.CreateJob(func() error {
				if count.Add(1) == 2 {
					cancel()
				}

				return nil
			}), time.Millisecond)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
			Expect(count.Load()).To(Equal(int32(2)))
		})

		It("should repeat while the condition is met", func(ctx SpecContext) {
			count := 0

			Expect(plumber.JobWhile(func() bool {
				return count < 3
			}, plumber.CreateJob(func() error {
				count++

				return nil
			}))(ctx)).To(Succeed())

			Expect(count).To(Equal(3))
		})

		It("should interrupt the sleep of the waiting job", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)

			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()

			err := plumber.JobWait(func() bool {
				return false
			}, time.Hour)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
		})
	})

	Describe("delay", func() {
		It("should not run the job when the flow is cancelled while waiting", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)

			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()

			ran := false

			err := plumber.JobDelay(plumber.CreateJob(func() error {
				ran = true

				return nil
			}), time.Hour)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
			Expect(ran).To(BeFalse())
		})
	})

	Describe("background", func() {
		It("should not wait for the job", func(ctx SpecContext) {
			started := make(chan struct{})
			release := make(chan struct{})

			Expect(plumber.JobBackground(func(_ context.Context) error {
				close(started)

				<-release

				return nil
			})(ctx)).To(Succeed())

			Eventually(started).Should(BeClosed())
			close(release)
		})

		It("should log and drop the error of the job", func(ctx SpecContext) {
			log, capture := plumbertests.NewCaptureLogger()

			Expect(plumber.JobBackground(plumber.CreateJob(func() error {
				return fmt.Errorf("failed")
			}), log)(ctx)).To(Succeed())

			Eventually(capture.Messages).Should(ContainElement("Background job has failed: failed"))
		})

		It("should be cancelled together with the flow around it", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)

			var count atomic.Int32

			Expect(plumber.JobBackground(plumber.JobLoopWithWaitAfter(plumber.CreateJob(func() error {
				count.Add(1)

				return nil
			}), time.Millisecond))(cancelled)).To(Succeed())

			Eventually(func() int32 {
				return count.Load()
			}).Should(BeNumerically(">", 1))

			cancel()

			time.Sleep(10 * time.Millisecond)

			settled := count.Load()

			Consistently(func() int32 {
				return count.Load()
			}, 50*time.Millisecond).Should(Equal(settled))
		})
	})
})
