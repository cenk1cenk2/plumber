package flow_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenk1cenk2/plumber/v6/internal/flow"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
)

type predicateCase struct {
	build func(flow.Predicate, flow.Predicate) flow.Predicate
}

var _ = Describe("jobs", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	DescribeTable("should compose predicates",
		func(tc predicateCase) {
			truthy := flow.Predicate(func() bool {
				return true
			})
			falsey := flow.Predicate(func() bool {
				return false
			})

			Expect(tc.build(truthy, falsey)()).To(BeTrue())
		},
		Entry("simple predicate", predicateCase{
			build: func(truthy flow.Predicate, _ flow.Predicate) flow.Predicate {
				return truthy
			},
		}),
		Entry("and", predicateCase{
			build: func(truthy flow.Predicate, _ flow.Predicate) flow.Predicate {
				return flow.PredicateAnd(truthy, truthy)
			},
		}),
		Entry("or", predicateCase{
			build: func(truthy flow.Predicate, falsey flow.Predicate) flow.Predicate {
				return flow.PredicateOr(falsey, truthy)
			},
		}),
		Entry("not", predicateCase{
			build: func(_ flow.Predicate, falsey flow.Predicate) flow.Predicate {
				return flow.PredicateNot(falsey)
			},
		}),
		Entry("xor", predicateCase{
			build: func(truthy flow.Predicate, falsey flow.Predicate) flow.Predicate {
				return flow.PredicateXor(truthy, falsey)
			},
		}),
	)

	It("should run basic jobs in sequence, parallel, and repeat", func() {
		var lock sync.Mutex
		order := []string{}
		appendOrder := func(value string) {
			lock.Lock()
			order = append(order, value)
			lock.Unlock()
		}

		Expect(flow.JobSequence(
			flow.CreateJob(func() error {
				appendOrder("one")

				return nil
			}),
			flow.JobParallel(
				flow.CreateJob(func() error {
					appendOrder("two")

					return nil
				}),
				flow.CreateJob(func() error {
					appendOrder("three")

					return nil
				}),
			),
			flow.JobRepeat(
				flow.CreateJob(func() error {
					appendOrder("repeat")

					return nil
				}),
				2,
			),
		)(ctx)).To(Succeed())
		Expect(order[0]).To(Equal("one"))
		Expect(order).To(ContainElements("two", "three", "repeat", "repeat"))
	})

	It("should branch and wait through helper jobs", func() {
		order := []string{}
		ready := false

		Expect(flow.JobSequence(
			flow.JobIf(
				func() bool {
					return true
				},
				flow.CreateJob(func() error {
					order = append(order, "then")

					return nil
				}),
				flow.CreateJob(func() error {
					order = append(order, "else")

					return nil
				}),
			),
			flow.JobIfNot(
				func() bool {
					return false
				},
				flow.CreateJob(func() error {
					order = append(order, "if-not")

					return nil
				}),
			),
			flow.JobDelay(flow.CreateJob(func() error {
				ready = true

				return nil
			}), time.Millisecond),
			flow.JobWait(func() bool {
				return ready
			}, time.Millisecond),
			flow.CreateEmptyJob(),
		)(ctx)).To(Succeed())
		Expect(order).To(Equal([]string{"then", "if-not"}))
	})

	It("should panic when the conditional job is not given a job", func() {
		Expect(func() {
			flow.JobIf(func() bool {
				return true
			})
		}).To(PanicWith(MatchError("Conditional job requires a job and optionally its alternative.")))

		Expect(func() {
			flow.JobIf(
				func() bool {
					return true
				},
				flow.CreateEmptyJob(),
				flow.CreateEmptyJob(),
				flow.CreateEmptyJob(),
			)
		}).To(PanicWith(MatchError("Conditional job requires a job and optionally its alternative.")))
	})

	Describe("sequence", func() {
		It("should stop on the first error", func() {
			ran := false

			err := flow.JobSequence(
				flow.CreateJob(func() error {
					return fmt.Errorf("failed")
				}),
				flow.CreateJob(func() error {
					ran = true

					return nil
				}),
			)(ctx)

			Expect(err).To(MatchError("failed"))
			Expect(ran).To(BeFalse())
		})

		It("should not run any job while the flow is cancelled", func() {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			ran := false

			err := flow.JobSequence(flow.CreateJob(func() error {
				ran = true

				return nil
			}))(cancelled)

			Expect(err).To(MatchError(context.Canceled))
			Expect(ran).To(BeFalse())
		})
	})

	Describe("parallel", func() {
		It("should return the first real error and cancel its siblings", func() {
			cancelled := make(chan struct{})

			err := flow.JobParallel(
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

		It("should return the error of the flow when everything is cancelled", func() {
			cancelled, cancel := context.WithCancel(ctx)

			err := flow.JobParallel(
				func(ctx context.Context) error {
					cancel()

					<-ctx.Done()

					return fmt.Errorf("killed the process")
				},
			)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
		})

		It("should wait for every job to finish", func() {
			var finished atomic.Int32

			Expect(flow.JobParallel(
				flow.JobDelay(flow.CreateJob(func() error {
					finished.Add(1)

					return nil
				}), time.Millisecond),
				flow.JobDelay(flow.CreateJob(func() error {
					finished.Add(1)

					return nil
				}), 5*time.Millisecond),
			)(ctx)).To(Succeed())

			Expect(finished.Load()).To(Equal(int32(2)))
		})
	})

	Describe("loops", func() {
		It("should stop looping when the job fails", func() {
			count := 0

			err := flow.JobLoop(flow.CreateJob(func() error {
				count++

				if count == 3 {
					return fmt.Errorf("failed")
				}

				return nil
			}))(ctx)

			Expect(err).To(MatchError("failed"))
			Expect(count).To(Equal(3))
		})

		It("should stop looping when the flow is cancelled", func() {
			cancelled, cancel := context.WithCancel(ctx)
			defer cancel()

			var count atomic.Int32

			err := flow.JobLoopWithWaitAfter(flow.CreateJob(func() error {
				if count.Add(1) == 2 {
					cancel()
				}

				return nil
			}), time.Millisecond)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
			Expect(count.Load()).To(Equal(int32(2)))
		})

		It("should repeat while the condition is met", func() {
			count := 0

			Expect(flow.JobWhile(func() bool {
				return count < 3
			}, flow.CreateJob(func() error {
				count++

				return nil
			}))(ctx)).To(Succeed())

			Expect(count).To(Equal(3))
		})

		It("should interrupt the sleep of the waiting job", func() {
			cancelled, cancel := context.WithCancel(ctx)

			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()

			err := flow.JobWait(func() bool {
				return false
			}, time.Hour)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
		})
	})

	Describe("delay", func() {
		It("should not run the job when the flow is cancelled while waiting", func() {
			cancelled, cancel := context.WithCancel(ctx)

			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()

			ran := false

			err := flow.JobDelay(flow.CreateJob(func() error {
				ran = true

				return nil
			}), time.Hour)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
			Expect(ran).To(BeFalse())
		})
	})

	Describe("background", func() {
		It("should not wait for the job", func() {
			started := make(chan struct{})
			release := make(chan struct{})

			Expect(flow.JobBackground(func(_ context.Context) error {
				close(started)

				<-release

				return nil
			})(ctx)).To(Succeed())

			Eventually(started).Should(BeClosed())
			close(release)
		})

		It("should log and drop the error of the job", func() {
			log, hook := logrustest.NewNullLogger()
			log.SetLevel(logrus.TraceLevel)

			Expect(flow.JobBackground(flow.CreateJob(func() error {
				return fmt.Errorf("failed")
			}), log)(ctx)).To(Succeed())

			Eventually(func() []string {
				messages := []string{}

				for _, entry := range hook.AllEntries() {
					messages = append(messages, entry.Message)
				}

				return messages
			}).Should(ContainElement("Background job has failed: failed"))
		})

		It("should be cancelled together with the flow around it", func() {
			cancelled, cancel := context.WithCancel(ctx)

			var count atomic.Int32

			Expect(flow.JobBackground(flow.JobLoopWithWaitAfter(flow.CreateJob(func() error {
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
