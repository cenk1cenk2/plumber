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
	It("should capture values in to the fields of a context", func(ctx SpecContext) {
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

	It("should capture the values of the jobs that run in parallel", func(ctx SpecContext) {
		c := captureContext{}

		Expect(plumber.JobParallel(
			plumber.Capture(&c.Version, func(_ context.Context) (string, error) {
				time.Sleep(time.Millisecond)

				return "v1.0.0", nil
			}),
			plumber.Capture(&c.Count, func(_ context.Context) (int, error) {
				return 2, nil
			}),
		)(ctx)).To(Succeed())

		Expect(c.Version).To(Equal("v1.0.0"))
		Expect(c.Count).To(Equal(2))
	})

	It("should not capture anything when the job fails", func(ctx SpecContext) {
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
	})
})
