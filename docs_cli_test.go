package plumber_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/cenk1cenk2/plumber/v7"
	plumbertests "github.com/cenk1cenk2/plumber/v7/tests"
	"github.com/urfave/cli/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type documentationCase struct {
	args        []string
	outputFlag  bool
	command     func(*plumber.Plumber) *cli.Command
	prepare     func(string)
	configure   func(*plumber.Plumber, string)
	contains    []string
	notContains []string
}

/*
Creates the application that exercises every piece of metadata that the documentation renders.

The fixture carries categorized and uncategorized flags, a required flag, the enum and the format
notations, a default text, positional arguments, categorized commands, a mutually exclusive group
and a category that is shared between two commands so that the repetition of it collapses.
*/
func documentationFixtureCommand(app *plumber.Plumber) *cli.Command {
	setup := func() []cli.Flag {
		return []cli.Flag{
			&cli.StringFlag{
				Name:     "cwd",
				Category: "Setup",
				Usage:    "Working directory of the commands.",
				Sources:  cli.EnvVars("FIXTURE_CWD"),
				Value:    ".",
			},
			&cli.StringFlag{
				Name:        "cache",
				Category:    "Setup",
				Usage:       "Cache directory of the commands.",
				Sources:     cli.EnvVars("FIXTURE_CACHE"),
				DefaultText: "the cache directory of the system",
			},
		}
	}

	return &cli.Command{
		Name:        "fixture",
		Description: "Fixture application for the documentation generator.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "verbose",
				Usage:   "Enable the verbose output.",
				Sources: cli.EnvVars("FIXTURE_VERBOSE"),
			},
			&cli.StringFlag{
				Name:     "color",
				Category: "Terminal",
				Usage:    `Colorize the output of the application. enum("always", "auto", "never")`,
				Sources:  cli.EnvVars("FIXTURE_COLOR"),
				Value:    "auto",
			},
		},
		Commands: []*cli.Command{
			plumber.DocsCommand(app),
			{
				Name:        "shell",
				Usage:       "Prints the completion script.",
				Description: "Uncategorized commands are always documented before the categorized ones.",
			},
			{
				Name:      "build",
				Aliases:   []string{"b"},
				Category:  "Pipeline",
				Usage:     "Builds the application.",
				ArgsUsage: "<target> [tags ...]",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:      "target",
						UsageText: "<target>",
					},
					&cli.StringArgs{
						Name: "tags",
						Min:  0,
						Max:  -1,
					},
				},
				Flags: append(
					setup(),
					&cli.StringFlag{
						Name:     "output",
						Category: "Build",
						Usage:    "Output directory of the artifacts.",
						Sources:  cli.EnvVars("FIXTURE_OUTPUT"),
						Required: true,
						Value:    "./dist/",
					},
					&cli.StringFlag{
						Name:     "naming",
						Category: "Build",
						Usage:    `Naming of the artifacts. format(Template(map[string]string))`,
						Value:    "{{ .name }}-{{ .os }}",
					},
					&cli.StringSliceFlag{
						Name:     "tag",
						Category: "Build",
						Usage:    "Tags of the build.",
					},
				),
				MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
					{
						Category: "Build",
						Flags: [][]cli.Flag{
							{&cli.BoolFlag{Name: "json", Category: "Build", Usage: "Report as json."}},
							{&cli.BoolFlag{Name: "yaml", Category: "Build", Usage: "Report as yaml."}},
						},
					},
				},
				Commands: []*cli.Command{
					{
						Name:  "cache",
						Usage: "Warms the cache of the build.",
						Flags: setup(),
					},
				},
			},
			{
				Name:      "lint",
				Category:  "Pipeline",
				Usage:     "Lints the application.",
				UsageText: "fixture lint [--fix] [FLAGS]",
				Flags: append(
					setup(),
					&cli.BoolFlag{
						Name:  "fix",
						Usage: "Fix the reported problems.",
					},
				),
			},
		},
	}
}

var _ = Describe("documentation and Cli runtime", func() {
	DescribeTable("should render markdown documentation files",
		func(_ SpecContext, tc documentationCase) {
			output := filepath.Join(plumbertests.TempDir(), "README.md")
			if tc.prepare != nil {
				tc.prepare(output)
			}

			fixture := plumbertests.NewPlumber(func(app *plumber.Plumber) *cli.Command {
				return tc.command(app)
			})
			log, _ := plumbertests.NewCaptureLogger()
			fixture.Plumber.Log = log
			tc.configure(fixture.Plumber, output)

			args := append([]string{}, tc.args...)
			if tc.outputFlag {
				args = append(args, "--output", output)
			}
			plumbertests.WithArgs(args...)

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
		Entry("docs markdown command", documentationCase{
			args: []string{"docs-test", "docs", "markdown"},
			command: func(app *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name:        "docs-test",
					Description: "Documentation test.",
					Commands: []*cli.Command{
						plumber.DocsCommand(app),
						{
							Name:        "visible",
							Description: "Visible command.",
						},
					},
				}
			},
			configure: func(app *plumber.Plumber, output string) {
				app.SetDocumentationOptions(plumber.DocumentationOptions{
					MarkdownOutputFile: output,
				})
			},
			contains:    []string{"docs-test", "visible"},
			notContains: []string{"docs markdown"},
		}),
		Entry("docs embed command", documentationCase{
			args: []string{"embed-test", "docs", "embed"},
			prepare: func(output string) {
				Expect(os.WriteFile(
					output,
					[]byte("before\n<!-- clidocs -->\nold\n<!-- clidocsstop -->\nafter\n"),
					0600,
				)).To(Succeed())
			},
			command: func(app *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name:     "embed-test",
					Commands: []*cli.Command{plumber.DocsCommand(app)},
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
		Entry("docs markdown command with an output override", documentationCase{
			args:       []string{"docs-test", "docs", "markdown"},
			outputFlag: true,
			command: func(app *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name:     "docs-test",
					Commands: []*cli.Command{plumber.DocsCommand(app)},
				}
			},
			configure: func(app *plumber.Plumber, output string) {
				app.SetDocumentationOptions(plumber.DocumentationOptions{
					MarkdownOutputFile: output + ".configured",
				})
			},
			contains: []string{"docs-test"},
		}),
		Entry("docs embed command with an output override", documentationCase{
			args:       []string{"embed-test", "docs", "embed"},
			outputFlag: true,
			prepare: func(output string) {
				Expect(os.WriteFile(
					output,
					[]byte("before\n<!-- clidocs -->\nold\n<!-- clidocsstop -->\nafter\n"),
					0600,
				)).To(Succeed())
			},
			command: func(app *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name:     "embed-test",
					Commands: []*cli.Command{plumber.DocsCommand(app)},
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
					EmbeddedMarkdownOutputFile: output + ".configured",
				})
			},
			contains:    []string{"before", "after", "--enabled"},
			notContains: []string{"old"},
		}),
	)

	It("should render the full style of the fixture application", func(_ SpecContext) {
		Expect(renderDocumentationFixture("markdown", "full")).To(Equal(documentationFixtureMarkdown))
	})

	It("should render the flags style as the full style without the headline", func(_ SpecContext) {
		Expect(renderDocumentationFixture("markdown", "flags")).To(Equal(
			strings.TrimPrefix(documentationFixtureMarkdown, documentationFixtureHeadline),
		))
	})

	It("should default the styles of the subcommands to the ones of their own", func(_ SpecContext) {
		Expect(renderDocumentationFixture("markdown")).To(Equal(documentationFixtureMarkdown))
		Expect(renderDocumentationFixture("embed")).To(ContainSubstring(
			strings.TrimSpace(strings.TrimPrefix(documentationFixtureMarkdown, documentationFixtureHeadline)),
		))
	})

	It("should embed the full style whenever it is selected", func(_ SpecContext) {
		Expect(renderDocumentationFixture("embed", "full")).To(ContainSubstring(strings.TrimSpace(documentationFixtureMarkdown)))
	})

	It("should refuse a style that is unknown", func(_ SpecContext) {
		output := filepath.Join(plumbertests.TempDir(), "README.md")

		fixture := plumbertests.NewPlumber(documentationFixtureCommand)
		log, _ := plumbertests.NewCaptureLogger()
		fixture.Plumber.Log = log
		fixture.Plumber.SetDocumentationOptions(plumber.DocumentationOptions{MarkdownOutputFile: output})

		plumbertests.WithArgs("fixture", "docs", "markdown", "--style", "nonsense")

		fixture.Plumber.Run()

		Expect(fixture.ExitCodes()).ToNot(BeEmpty())
		Expect(output).ToNot(BeAnExistingFile())
	})

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
})

// Runs the documentation of the fixture application through the given subcommand and style.
func renderDocumentationFixture(subcommand string, style ...string) string {
	GinkgoHelper()

	output := filepath.Join(plumbertests.TempDir(), "README.md")

	if subcommand == "embed" {
		Expect(os.WriteFile(
			output,
			[]byte("before\n<!-- clidocs -->\nold\n<!-- clidocsstop -->\nafter\n"),
			0600,
		)).To(Succeed())
	}

	fixture := plumbertests.NewPlumber(documentationFixtureCommand)
	log, _ := plumbertests.NewCaptureLogger()
	fixture.Plumber.Log = log
	fixture.Plumber.SetDocumentationOptions(plumber.DocumentationOptions{
		MarkdownOutputFile:         output,
		EmbeddedMarkdownOutputFile: output,
	})

	args := []string{"fixture", "docs", subcommand}
	if len(style) > 0 {
		args = append(args, "--style", style[0])
	}

	plumbertests.WithArgs(args...)

	fixture.Plumber.Run()

	data, err := os.ReadFile(output)
	Expect(err).ToNot(HaveOccurred())

	return string(data)
}

// The headline of the full style, which is the only thing that the flags style leaves out.
var documentationFixtureHeadline = strings.ReplaceAll(`# fixture

Fixture application for the documentation generator.

'fixture [GLOBAL FLAGS] [COMMAND] [FLAGS]'

`, "'", "`")

/*
The documentation that the fixture application renders as the full style.

The golden document is written with the apostrophe in the place of the backtick since a raw string
literal of Go can not carry a backtick of its own.
*/
var documentationFixtureMarkdown = strings.ReplaceAll(`# fixture

Fixture application for the documentation generator.

'fixture [GLOBAL FLAGS] [COMMAND] [FLAGS]'

## Global Flags

| Flag / Environment | Description | Type | Default |
| --- | --- | --- | --- |
| '--verbose'<br/>'$FIXTURE_VERBOSE' | Enable the verbose output. | 'bool' | 'false' |

**CLI**

| Flag / Environment | Description | Type | Default |
| --- | --- | --- | --- |
| '--log-level'<br/>'$LOG_LEVEL' | Define the log level for the application. | 'string'<br/>'enum("panic", "fatal", "warn", "info", "debug", "trace")' | '"info"' |
| '--env-file'<br/>'$ENV_FILE' | Environment files to inject. | 'string[]' |  |

**Terminal**

| Flag / Environment | Description | Type | Default |
| --- | --- | --- | --- |
| '--color'<br/>'$FIXTURE_COLOR' | Colorize the output of the application. | 'string'<br/>'enum("always", "auto", "never")' | '"auto"' |

## Commands

- ['fixture shell'](#fixture-shell)
- ['fixture build'](#fixture-build-b)
  - ['fixture build cache'](#fixture-build-cache)
- ['fixture lint'](#fixture-lint)

### 'fixture shell'

Prints the completion script.

Uncategorized commands are always documented before the categorized ones.

'fixture shell'

### Pipeline

#### 'fixture build', 'b'

Builds the application.

'fixture build [FLAGS] <target> [tags ...]'

##### Arguments

| Argument | Usage | Type | Values |
| --- | --- | --- | --- |
| 'target' | '<target>' | 'string' | '1' |
| 'tags' | '[tags ...]' | 'string[]' | '0..*' |

##### Flags

**Build**

| Flag / Environment | Description | Type | Default |
| --- | --- | --- | --- |
| **'--output'**<br/>**'$FIXTURE_OUTPUT'**\* | Output directory of the artifacts. | 'string' | '"./dist/"' |
| '--naming' | Naming of the artifacts. | 'string'<br/>'format(Template(map[string]string))' | '"{{ .name }}-{{ .os }}"' |
| '--tag' | Tags of the build. | 'string[]' |  |
| '--json' (1) | Report as json. | 'bool' | 'false' |
| '--yaml' (1) | Report as yaml. | 'bool' | 'false' |

\* required

(1) mutually exclusive

**Setup**

| Flag / Environment | Description | Type | Default |
| --- | --- | --- | --- |
| '--cwd'<br/>'$FIXTURE_CWD' | Working directory of the commands. | 'string' | '"."' |
| '--cache'<br/>'$FIXTURE_CACHE' | Cache directory of the commands. | 'string' | 'the cache directory of the system' |

##### 'fixture build cache'

Warms the cache of the build.

'fixture build cache [FLAGS]'

###### Flags

<details>
<summary>Setup</summary>

| Flag / Environment | Description | Type | Default |
| --- | --- | --- | --- |
| '--cwd'<br/>'$FIXTURE_CWD' | Working directory of the commands. | 'string' | '"."' |
| '--cache'<br/>'$FIXTURE_CACHE' | Cache directory of the commands. | 'string' | 'the cache directory of the system' |

</details>

#### 'fixture lint'

Lints the application.

'fixture lint [--fix] [FLAGS]'

##### Flags

| Flag / Environment | Description | Type | Default |
| --- | --- | --- | --- |
| '--fix' | Fix the reported problems. | 'bool' | 'false' |

<details>
<summary>Setup</summary>

| Flag / Environment | Description | Type | Default |
| --- | --- | --- | --- |
| '--cwd'<br/>'$FIXTURE_CWD' | Working directory of the commands. | 'string' | '"."' |
| '--cache'<br/>'$FIXTURE_CACHE' | Cache directory of the commands. | 'string' | 'the cache directory of the system' |

</details>
`, "'", "`")
