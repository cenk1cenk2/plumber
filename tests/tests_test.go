package tests_test

import (
	"context"
	"os"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("test helpers", func() {
	It("should create a plumber fixture with ginkgo debug logging", func() {
		fixture := plumbertests.NewPlumber()

		fixture.Plumber.Log.Info("hello")

		Expect(fixture.Plumber.Cli.Name).To(Equal("plumber-test"))
		Expect(fixture.Plumber.Log.Out).To(Equal(GinkgoWriter))
		Expect(fixture.Plumber.Log.GetLevel()).To(Equal(logrus.DebugLevel))
	})

	It("should set process arguments for the current spec", func() {
		plumbertests.WithArgs("plumber", "test")

		Expect(os.Args).To(Equal([]string{"plumber", "test"}))
	})

	It("should create strict mockery command runners for the current spec", func() {
		runner := plumbertests.NewMockCommandRunner()
		result := plumbertests.TestingCommandSuccess()
		runner.EXPECT().
			Run(
				mock.Anything,
				mock.MatchedBy(func(invocation plumber.CommandInvocation) bool {
					return invocation.Name == "mock"
				}),
				mock.Anything,
			).
			Return(result, nil).
			Once()

		actual, err := runner.Run(
			context.Background(),
			plumber.CommandInvocation{Name: "mock"},
			plumber.CommandRuntime{},
		)

		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(result))
	})

	It("should set environment variables for the current spec", func() {
		plumbertests.WithEnvironment(map[string]string{
			"PLUMBER_TEST_HELPER": "enabled",
		})

		Expect(os.Getenv("PLUMBER_TEST_HELPER")).To(Equal("enabled"))
	})

	It("should unset environment variables for the current spec", func() {
		plumbertests.WithEnvironment(map[string]string{
			"PLUMBER_TEST_HELPER": "enabled",
		})

		plumbertests.WithoutEnvironment("PLUMBER_TEST_HELPER")

		_, existed := os.LookupEnv("PLUMBER_TEST_HELPER")
		Expect(existed).To(BeFalse())
	})

	It("should prepend paths for the current spec", func() {
		previousPath := os.Getenv("PATH")

		plumbertests.WithPath("/tmp/plumber-bin")

		Expect(os.Getenv("PATH")).To(HavePrefix("/tmp/plumber-bin" + string(os.PathListSeparator)))
		Expect(os.Getenv("PATH")).To(ContainSubstring(previousPath))
	})

	It("should change the working directory for the current spec", func() {
		dir := plumbertests.TempDir()
		previousDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())

		Expect(plumbertests.WithWorkingDirectory(dir)).To(Equal(previousDir))

		currentDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		Expect(currentDir).To(Equal(dir))
	})

	It("should create and enter temporary working directories", func() {
		previousDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())

		dir := plumbertests.WithTempWorkingDirectory("nested")

		currentDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		Expect(currentDir).To(Equal(dir))
		Expect(currentDir).ToNot(Equal(previousDir))
	})
})
