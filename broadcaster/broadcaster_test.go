package broadcaster_test

import (
	"github.com/cenk1cenk2/plumber/v6/broadcaster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Broadcaster", func() {
	It("should broadcast submitted values to every registered channel", func() {
		b := broadcaster.NewBroadcaster[string](1)
		defer b.Close()

		first := make(chan string, 1)
		second := make(chan string, 1)

		b.Register(first)
		b.Register(second)
		b.Submit("message")

		Eventually(first).Should(Receive(Equal("message")))
		Eventually(second).Should(Receive(Equal("message")))
	})

	It("should stop broadcasting to unregistered channels", func() {
		b := broadcaster.NewBroadcaster[string](1)
		defer b.Close()

		registered := make(chan string, 1)
		removed := make(chan string, 1)

		b.Register(registered)
		b.Register(removed)
		b.Unregister(removed)
		b.Submit("message")

		Eventually(registered).Should(Receive(Equal("message")))
		Consistently(removed).ShouldNot(Receive())
	})

	It("should report whether values can be submitted without blocking", func() {
		b := broadcaster.NewBroadcaster[string](1)
		defer b.Close()

		Expect(b.TrySubmit("message")).To(BeTrue())
	})
})
