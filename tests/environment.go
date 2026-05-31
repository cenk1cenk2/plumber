package tests

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TempDir(paths ...string) string {
	GinkgoHelper()

	dir := GinkgoT().TempDir()

	if len(paths) == 0 {
		return dir
	}

	target := filepath.Join(append([]string{dir}, paths...)...)
	Expect(os.MkdirAll(target, 0700)).To(Succeed())

	return target
}

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

func WithoutEnvironment(keys ...string) {
	GinkgoHelper()

	for _, key := range keys {
		previousValue, existed := os.LookupEnv(key)

		Expect(os.Unsetenv(key)).To(Succeed())

		DeferCleanup(func() {
			if existed {
				Expect(os.Setenv(key, previousValue)).To(Succeed())

				return
			}

			Expect(os.Unsetenv(key)).To(Succeed())
		})
	}
}

func WithPath(paths ...string) {
	GinkgoHelper()

	parts := append([]string{}, paths...)
	if current := os.Getenv("PATH"); current != "" {
		parts = append(parts, current)
	}

	WithEnvironment(map[string]string{
		"PATH": strings.Join(parts, string(os.PathListSeparator)),
	})
}

func WithWorkingDirectory(dirs ...string) string {
	GinkgoHelper()

	var dir string
	if len(dirs) > 0 && dirs[0] != "" {
		dir = dirs[0]
	} else {
		dir = TempDir()
	}

	previousDir, err := os.Getwd()
	Expect(err).ToNot(HaveOccurred())
	Expect(os.Chdir(dir)).To(Succeed())

	DeferCleanup(func() {
		Expect(os.Chdir(previousDir)).To(Succeed())
	})

	return previousDir
}

func WithTempWorkingDirectory(paths ...string) string {
	GinkgoHelper()

	dir := TempDir(paths...)
	WithWorkingDirectory(dir)

	return dir
}
