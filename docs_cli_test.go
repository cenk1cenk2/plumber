package plumber_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	"github.com/urfave/cli/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("documentation and CLI runtime", func() {
	It("should generate markdown documentation into a ginkgo temp file", func() {
		output := filepath.Join(plumbertests.TempDir(), "README.md")
		fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
			return &cli.Command{
				Name:        "docs-test",
				Description: "Documentation test.",
				Commands: []*cli.Command{
					{
						Name:        "visible",
						Description: "Visible command.",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:  "name",
								Usage: "Name flag.",
								Value: "plumber",
							},
						},
					},
				},
			}
		})
		fixture.Plumber.SetDocumentationOptions(plumber.DocumentationOptions{
			MarkdownOutputFile: output,
		})
		plumbertests.WithArgs("docs-test", "MARKDOWN_DOC")

		fixture.Plumber.Run()

		data, err := os.ReadFile(output)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("docs-test"))
		Expect(string(data)).To(ContainSubstring("visible"))
		Expect(string(data)).To(ContainSubstring("--name"))
	})

	It("should embed markdown documentation between markers", func() {
		output := filepath.Join(plumbertests.TempDir(), "README.md")
		Expect(os.WriteFile(
			output,
			[]byte("before\n<!-- clidocs -->\nold\n<!-- clidocsstop -->\nafter\n"),
			0600,
		)).To(Succeed())
		fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
			return &cli.Command{
				Name: "embed-test",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "enabled",
						Usage: "Enable the thing.",
					},
				},
			}
		})
		fixture.Plumber.SetDocumentationOptions(plumber.DocumentationOptions{
			EmbeddedMarkdownOutputFile: output,
		})
		plumbertests.WithArgs("embed-test", "MARKDOWN_EMBED")

		fixture.Plumber.Run()

		data, err := os.ReadFile(output)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("before"))
		Expect(string(data)).To(ContainSubstring("after"))
		Expect(string(data)).To(ContainSubstring("--enabled"))
		Expect(string(data)).ToNot(ContainSubstring("old"))
	})

	It("should load env files and run CLI setup before actions", func() {
		dir := plumbertests.TempDir()
		envFile := filepath.Join(dir, ".env")
		Expect(os.WriteFile(envFile, []byte("PLUMBER_ENV_FILE_TEST=loaded\n"), 0600)).To(Succeed())
		plumbertests.WithEnvironment(map[string]string{
			"ENV_FILE": envFile,
		})
		plumbertests.WithArgs("runtime-test", "--debug", "run")
		loaded := ""
		fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
			return &cli.Command{
				Name: "runtime-test",
				Commands: []*cli.Command{
					{
						Name: "run",
						Action: func(_ context.Context, _ *cli.Command) error {
							loaded = os.Getenv("PLUMBER_ENV_FILE_TEST")

							return nil
						},
					},
				},
			}
		})

		fixture.Plumber.Run()

		Expect(loaded).To(Equal("loaded"))
		Expect(fixture.Plumber.Environment.Debug).To(BeTrue())
	})

	It("should allow non-fatal deprecation notices during CLI setup", func() {
		plumbertests.WithEnvironment(map[string]string{
			"PLUMBER_DEPRECATED_ENV": "1",
		})
		plumbertests.WithArgs("deprecation-test", "run")
		ran := false
		fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
			return &cli.Command{
				Name: "deprecation-test",
				Commands: []*cli.Command{
					{
						Name: "run",
						Action: func(_ context.Context, _ *cli.Command) error {
							ran = true

							return nil
						},
					},
				},
			}
		})
		fixture.Plumber.SetDeprecationNotices([]plumber.DeprecationNotice{
			{
				Environment: []string{"PLUMBER_DEPRECATED_ENV"},
				Level:       plumber.LOG_LEVEL_WARN,
			},
		})

		fixture.Plumber.Run()

		Expect(ran).To(BeTrue())
	})
})
