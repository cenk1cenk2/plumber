package tests_test

import (
	"os"

	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("test helpers", func() {
	It("should create a plumber fixture with captured logger output", func() {
		fixture := plumbertests.NewPlumber()

		fixture.Plumber.Log.Info("hello")

		Expect(fixture.Plumber.Cli.Name).To(Equal("plumber-test"))
		Expect(fixture.Output.String()).To(ContainSubstring("hello"))
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

	It("should change the working directory for the current spec", func() {
		dir := GinkgoT().TempDir()
		previousDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())

		Expect(plumbertests.WithWorkingDirectory(dir)).To(Equal(previousDir))

		currentDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		Expect(currentDir).To(Equal(dir))
	})
})
