package broadcaster_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBroadcaster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Broadcaster Suite")
}
