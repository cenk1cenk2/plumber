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

type documentationCase struct {
	args        []string
	command     func() *cli.Command
	prepare     func(string)
	configure   func(*plumber.Plumber, string)
	contains    []string
	notContains []string
}

var _ = Describe("documentation and Cli runtime", func() {
	DescribeTable("should render markdown documentation files",
		func(tc documentationCase) {
			output := filepath.Join(plumbertests.TempDir(), "README.md")
			if tc.prepare != nil {
				tc.prepare(output)
			}

			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return tc.command()
			})
			tc.configure(fixture.Plumber, output)
			plumbertests.WithArgs(tc.args...)

			fixture.Plumber.Run()

			data, err := os.ReadFile(output)
			Expect(err).ToNot(HaveOccurred())
			for _, content := range tc.contains {
				Expect(string(data)).To(ContainSubstring(content))
			}
			for _, content := range tc.notContains {
				Expect(string(data)).ToNot(ContainSubstring(content))
			}
		},
		Entry("standalone markdown output", documentationCase{
			args: []string{"docs-test", "MARKDOWN_DOC"},
			command: func() *cli.Command {
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
			},
			configure: func(app *plumber.Plumber, output string) {
				app.SetDocumentationOptions(plumber.DocumentationOptions{
					MarkdownOutputFile: output,
				})
			},
			contains: []string{"docs-test", "visible", "--name"},
		}),
		Entry("embedded markdown output", documentationCase{
			args: []string{"embed-test", "MARKDOWN_EMBED"},
			prepare: func(output string) {
				Expect(os.WriteFile(
					output,
					[]byte("before\n<!-- clidocs -->\nold\n<!-- clidocsstop -->\nafter\n"),
					0600,
				)).To(Succeed())
			},
			command: func() *cli.Command {
				return &cli.Command{
					Name: "embed-test",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:  "enabled",
							Usage: "Enable the thing.",
						},
					},
				}
			},
			configure: func(app *plumber.Plumber, output string) {
				app.SetDocumentationOptions(plumber.DocumentationOptions{
					EmbeddedMarkdownOutputFile: output,
				})
			},
			contains:    []string{"before", "after", "--enabled"},
			notContains: []string{"old"},
		}),
	)

	It("should load env files and run Cli setup before actions", func() {
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

	It("should allow non-fatal deprecation notices during Cli setup", func() {
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
