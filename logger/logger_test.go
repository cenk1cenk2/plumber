package logger_test

import (
	"github.com/cenk1cenk2/plumber/v6/logger"
	"github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("InitiateLogger", func() {
	BeforeEach(func() {
		previousLogger := logger.Log
		logger.Log = nil

		DeferCleanup(func() {
			logger.Log = previousLogger
		})
	})

	It("should initialize the package logger once", func() {
		first := logger.InitiateLogger(logrus.DebugLevel)
		second := logger.InitiateLogger(logrus.ErrorLevel)

		Expect(second).To(BeIdenticalTo(first))
		Expect(second.Level).To(Equal(logrus.DebugLevel))
		Expect(logger.Log).To(BeIdenticalTo(first))
	})
})
