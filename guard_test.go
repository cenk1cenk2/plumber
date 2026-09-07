package plumber_test

import (
	"context"
	"fmt"
	"time"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type guardCase struct {
	job    func(*bool) plumber.Job
	assert func(bool)
}

var _ = Describe("guards", func() {

	DescribeTable("should guard jobs",
		func(ctx SpecContext, tc guardCase) {
			handled := false

			Expect(tc.job(&handled)(ctx)).To(Succeed())

			if tc.assert != nil {
				tc.assert(handled)
			}
		},
		Entry("ignore panic", guardCase{
			job: func(_ *bool) plumber.Job {
				return plumber.GuardIgnorePanic(plumber.CreateJob(func() error {
					panic("ignored")
				}))
			},
		}),
		Entry("handle panic", guardCase{
			job: func(handled *bool) plumber.Job {
				return plumber.GuardOnPanic(plumber.CreateJob(func() error {
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
				return plumber.GuardResume(plumber.CreateJob(func() error {
					return fmt.Errorf("failed")
				}))
			},
		}),
		Entry("timeout successful job", guardCase{
			job: func(_ *bool) plumber.Job {
				return plumber.GuardTimeout(plumber.CreateJob(func() error {
					return nil
				}), time.Millisecond)
			},
		}),
	)

	Describe("panic", func() {
		It("should turn the panic into an error", func(ctx SpecContext) {
			err := plumber.GuardPanic(plumber.CreateJob(func() error {
				panic("exploded")
			}))(ctx)

			Expect(err).To(MatchError("Job has panicked: exploded"))
		})

		It("should keep the error of the panic wrapped", func(ctx SpecContext) {
			inner := fmt.Errorf("exploded")

			err := plumber.GuardPanic(plumber.CreateJob(func() error {
				panic(inner)
			}))(ctx)

			Expect(err).To(MatchError(inner))
		})
	})

	Describe("timeout", func() {
		It("should stop waiting for the job when the time is out", func(ctx SpecContext) {
			err := plumber.GuardTimeout(func(ctx context.Context) error {
				<-ctx.Done()

				return ctx.Err()
			}, time.Millisecond)(ctx)

			Expect(err).To(MatchError(context.DeadlineExceeded))
		})

		It("should call the handler when the time is out", func(ctx SpecContext) {
			handled := false

			err := plumber.GuardOnTimeout(func(ctx context.Context) error {
				<-ctx.Done()

				return ctx.Err()
			}, func() {
				handled = true
			}, time.Millisecond)(ctx)

			Expect(err).To(MatchError(context.DeadlineExceeded))
			Expect(handled).To(BeTrue())
		})

		It("should return the error of the flow when it is cancelled before the time is out", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)

			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()

			err := plumber.GuardTimeout(func(ctx context.Context) error {
				<-ctx.Done()

				return ctx.Err()
			}, time.Hour)(cancelled)

			Expect(err).To(MatchError(context.Canceled))
		})
	})

	Describe("ignore cancel", func() {
		It("should pass through the errors that are not caused by cancellation", func(ctx SpecContext) {
			err := plumber.GuardIgnoreCancel(plumber.CreateJob(func() error {
				return fmt.Errorf("failed")
			}))(ctx)

			Expect(err).To(MatchError("failed"))
		})

		It("should swallow the errors that do not even wrap the cancellation", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			Expect(plumber.GuardIgnoreCancel(plumber.CreateJob(func() error {
				return fmt.Errorf("signal: killed")
			}))(cancelled)).To(Succeed())
		})

		It("should stay interruptible", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)

			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()

			Expect(plumber.GuardIgnoreCancel(func(ctx context.Context) error {
				<-ctx.Done()

				return fmt.Errorf("signal: killed")
			})(cancelled)).To(Succeed())
		})
	})

	Describe("resume", func() {
		It("should log and swallow every error", func(ctx SpecContext) {
			log, capture := plumbertests.NewCaptureLogger()

			Expect(plumber.GuardResume(plumber.CreateJob(func() error {
				return fmt.Errorf("failed")
			}), log)(ctx)).To(Succeed())

			Expect(capture.Messages()).To(ContainElement("Job has failed, resuming the flow: failed"))
		})
	})

	Describe("always", func() {
		It("should run the job even when the flow is already cancelled", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			ran := false

			Expect(plumber.GuardAlways(func(ctx context.Context) error {
				ran = ctx.Err() == nil

				return nil
			})(cancelled)).To(Succeed())

			Expect(ran).To(BeTrue())
		})

		It("should pass through the errors of the job", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			err := plumber.GuardAlways(plumber.CreateJob(func() error {
				return fmt.Errorf("failed")
			}))(cancelled)

			Expect(err).To(MatchError("failed"))
		})

		It("should swallow the errors that are caused by the grace period running out", func(ctx SpecContext) {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			Expect(plumber.GuardAlways(func(ctx context.Context) error {
				<-ctx.Done()

				return fmt.Errorf("signal: killed")
			}, time.Millisecond)(cancelled)).To(Succeed())
		})
	})
})
