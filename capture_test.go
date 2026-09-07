package plumber_test

import (
	"context"
	"fmt"
	"time"

	"github.com/cenk1cenk2/plumber/v6"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type captureContext struct {
	Version string
	Count   int
}

var _ = Describe("capture", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should capture values in to the fields of a context", func() {
		c := captureContext{}

		Expect(plumber.JobSequence(
			plumber.Capture(&c.Version, func(_ context.Context) (string, error) {
				return "v1.0.0", nil
			}),
			plumber.Capture(&c.Count, func(_ context.Context) (int, error) {
				return 2, nil
			}),
			plumber.CreateJob(func() error {
				c.Version += "-tagged"

				return nil
			}),
		)(ctx)).To(Succeed())

		Expect(c.Version).To(Equal("v1.0.0-tagged"))
		Expect(c.Count).To(Equal(2))
	})

	It("should not capture anything when the job fails", func() {
		c := captureContext{Version: "v0.0.0", Count: 0}

		err := plumber.Capture(&c.Version, func(_ context.Context) (string, error) {
			return "v1.0.0", fmt.Errorf("failed")
		})(ctx)

		Expect(err).To(MatchError("failed"))
		Expect(c.Version).To(Equal("v0.0.0"))
	})

	It("should panic without a destination", func() {
		Expect(func() {
			plumber.Capture(nil, func(_ context.Context) (string, error) {
				return "", nil
			})
		}).To(PanicWith(MatchError("Captured value requires a destination.")))

		Expect(func() {
			plumber.CaptureResult(nil, func(_ context.Context) (string, error) {
				return "", nil
			})
		}).To(PanicWith(MatchError("Captured result requires a destination.")))
	})

	Describe("result", func() {
		It("should carry the values of the jobs that run in parallel", func() {
			version := plumber.Result[string]{}
			count := plumber.Result[int]{}

			Expect(plumber.JobSequence(
				plumber.JobParallel(
					plumber.CaptureResult(&version, func(_ context.Context) (string, error) {
						time.Sleep(time.Millisecond)

						return "v1.0.0", nil
					}),
					plumber.CaptureResult(&count, func(_ context.Context) (int, error) {
						return 2, nil
					}),
				),
				plumber.CreateJob(func() error {
					if version.Get() != "v1.0.0" {
						return fmt.Errorf("Captured version is not available.")
					}

					return nil
				}),
			)(ctx)).To(Succeed())

			Expect(version.Get()).To(Equal("v1.0.0"))
			Expect(count.Get()).To(Equal(2))
		})

		It("should report whether it is captured at all", func() {
			result := plumber.Result[string]{}

			value, ok := result.Ok()

			Expect(value).To(BeEmpty())
			Expect(ok).To(BeFalse())

			Expect(plumber.CaptureResult(&result, func(_ context.Context) (string, error) {
				return "", nil
			})(ctx)).To(Succeed())

			value, ok = result.Ok()

			Expect(value).To(BeEmpty())
			Expect(ok).To(BeTrue())
		})

		It("should not capture anything when the job fails", func() {
			result := plumber.Result[string]{}

			err := plumber.CaptureResult(&result, func(_ context.Context) (string, error) {
				return "v1.0.0", fmt.Errorf("failed")
			})(ctx)

			Expect(err).To(MatchError("failed"))

			_, ok := result.Ok()

			Expect(ok).To(BeFalse())
		})
	})
})
