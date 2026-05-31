package tests_test

import (
	"os"

	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
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

	It("should set environment variables for the current spec", func() {
		plumbertests.WithEnvironment(map[string]string{
			"PLUMBER_TEST_HELPER": "enabled",
		})

		Expect(os.Getenv("PLUMBER_TEST_HELPER")).To(Equal("enabled"))
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
