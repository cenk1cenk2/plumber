package tests

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func WithArgs(args ...string) {
	GinkgoHelper()

	previousArgs := append([]string{}, os.Args...)
	os.Args = append([]string{}, args...)

	DeferCleanup(func() {
		os.Args = previousArgs
	})
}

func WithEnvironment(environment map[string]string) {
	GinkgoHelper()

	for key, value := range environment {
		previousValue, existed := os.LookupEnv(key)

		Expect(os.Setenv(key, value)).To(Succeed())

		DeferCleanup(func() {
			if existed {
				Expect(os.Setenv(key, previousValue)).To(Succeed())

				return
			}

			Expect(os.Unsetenv(key)).To(Succeed())
		})
	}
}

func WithWorkingDirectory(dir string) string {
	GinkgoHelper()

	previousDir, err := os.Getwd()
	Expect(err).ToNot(HaveOccurred())
	Expect(os.Chdir(dir)).To(Succeed())

	DeferCleanup(func() {
		Expect(os.Chdir(previousDir)).To(Succeed())
	})

	return previousDir
}
