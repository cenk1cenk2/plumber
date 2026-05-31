package plumber_test

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	"github.com/urfave/cli/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("plumber", func() {
	Describe("construction", func() {
		It("should create a CLI application with default flags", func() {
			var provided *plumber.Plumber

			fixture := plumbertests.NewPlumber(func(p *plumber.Plumber) *cli.Command {
				provided = p

				return &cli.Command{
					Name:    "custom",
					Version: "v1",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: "custom"},
					},
				}
			})

			Expect(provided).To(BeIdenticalTo(fixture.Plumber))
			Expect(fixture.Plumber.Cli.Name).To(Equal("custom"))
			Expect(fixture.Plumber.Validator).ToNot(BeNil())
			Expect(fixture.Plumber.Log).ToNot(BeNil())

			flagNames := []string{}
			for _, flag := range fixture.Plumber.Cli.Flags {
				flagNames = append(flagNames, flag.Names()...)
			}

			Expect(flagNames).To(ContainElements("ci", "debug", "log-level", "env-file", "custom"))
		})

		It("should redact appended secrets from logger output", func() {
			fixture := plumbertests.NewPlumber()

			fixture.Plumber.AppendSecrets("secret-token")
			fixture.Plumber.Log.Info("using secret-token")

			Expect(fixture.Output.String()).To(ContainSubstring("[REDACTED]"))
			Expect(fixture.Output.String()).ToNot(ContainSubstring("secret-token"))
		})
	})

	Describe("validation", func() {
		type defaultedConfig struct {
			Name string `default:"worker" validate:"required"`
		}

		type invalidConfig struct {
			Name string `validate:"required"`
		}

		It("should apply defaults before validating data", func() {
			fixture := plumbertests.NewPlumber()
			config := &defaultedConfig{}

			Expect(fixture.Plumber.Validate(config)).To(Succeed())
			Expect(config.Name).To(Equal("worker"))
		})

		It("should return the current validation error message", func() {
			fixture := plumbertests.NewPlumber()

			Expect(fixture.Plumber.Validate(&invalidConfig{})).To(MatchError("Validation failed."))
		})
	})

	Describe("templates", func() {
		It("should execute slim sprig and custom template functions", func() {
			result, err := plumber.InlineTemplate(
				`{{ .Name | upper }} {{ shout .Suffix }}`,
				map[string]string{
					"Name":   "plumber",
					"Suffix": "tests",
				},
				template.FuncMap{
					"shout": strings.ToUpper,
				},
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("PLUMBER TESTS"))
		})

		It("should return an empty string for empty templates", func() {
			result, err := plumber.InlineTemplate("", map[string]string{})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeEmpty())
		})

		It("should collect template errors while returning successful results", func() {
			results, err := plumber.InlineTemplates([]string{
				"hello {{ . }}",
				"{{",
				"bye {{ . }}",
			}, "plumber")

			Expect(err).To(HaveOccurred())
			Expect(results).To(Equal([]string{"hello plumber", "", "bye plumber"}))
		})
	})

	Describe("environment helpers", func() {
		It("should parse current environment into a map", func() {
			plumbertests.WithEnvironment(map[string]string{
				"PLUMBER_TEST_ENV": "from-env",
			})

			Expect(plumber.ParseEnvironmentVariablesToMap()).To(HaveKeyWithValue("PLUMBER_TEST_ENV", "from-env"))
		})
	})

	Describe("CLI flag and argument helpers", func() {
		It("should edit a cloned flag slice without modifying the original slice", func() {
			flags := []cli.Flag{
				&cli.StringFlag{Name: "name", Value: "before"},
			}

			edited := plumber.EditCliFlag[*cli.StringFlag](
				flags,
				func(f *cli.StringFlag) bool {
					return f.Name == "name"
				},
				func(f *cli.StringFlag) *cli.StringFlag {
					clone := *f
					clone.Value = "after"

					return &clone
				},
			)

			originalFlag, ok := flags[0].(*cli.StringFlag)
			Expect(ok).To(BeTrue())
			editedFlag, ok := edited[0].(*cli.StringFlag)
			Expect(ok).To(BeTrue())

			Expect(originalFlag.Value).To(Equal("before"))
			Expect(editedFlag.Value).To(Equal("after"))
		})

		It("should panic when overwriting a missing flag", func() {
			flags := []cli.Flag{
				&cli.StringFlag{Name: "name"},
			}

			Expect(func() {
				plumber.OverwriteCliFlag[*cli.BoolFlag](
					flags,
					func(_ *cli.BoolFlag) bool {
						return true
					},
					func(f *cli.BoolFlag) *cli.BoolFlag {
						return f
					},
				)
			}).To(PanicWith(MatchError("Flag can not be found to modify.")))
		})

		It("should combine flag and argument slices in order", func() {
			flags := plumber.CombineFlags(
				[]cli.Flag{&cli.StringFlag{Name: "first"}},
				[]cli.Flag{&cli.BoolFlag{Name: "second"}},
			)
			arguments := plumber.CombineArguments(
				[]cli.Argument{&cli.StringArg{Name: "source"}},
				[]cli.Argument{&cli.StringArg{Name: "target"}},
			)

			Expect(flags).To(HaveLen(2))
			Expect(flags[0].Names()).To(ContainElement("first"))
			Expect(flags[1].Names()).To(ContainElement("second"))
			Expect(arguments).To(HaveLen(2))
			Expect(arguments[0].HasName("source")).To(BeTrue())
			Expect(arguments[1].HasName("target")).To(BeTrue())
		})
	})

	Describe("tasks", func() {
		It("should run task hooks and body in order", func() {
			fixture := plumbertests.NewPlumber()
			task := fixture.NewTaskList("tasks").CreateTask("deploy")
			order := []string{}

			task.
				ShouldRunBefore(func(_ *plumber.Task) error {
					order = append(order, "before")

					return nil
				}).
				Set(func(_ *plumber.Task) error {
					order = append(order, "run")

					return nil
				}).
				ShouldRunAfter(func(_ *plumber.Task) error {
					order = append(order, "after")

					return nil
				})

			Expect(task.Run()).To(Succeed())
			Expect(order).To(Equal([]string{"before", "run", "after"}))
		})

		It("should skip disabled tasks without running hooks", func() {
			fixture := plumbertests.NewPlumber()
			task := fixture.NewTaskList("tasks").CreateTask("disabled")
			order := []string{}

			task.
				ShouldDisable(func(_ *plumber.Task) bool {
					return true
				}).
				ShouldRunBefore(func(_ *plumber.Task) error {
					order = append(order, "before")

					return nil
				}).
				Set(func(_ *plumber.Task) error {
					order = append(order, "run")

					return nil
				})

			Expect(task.Run()).To(Succeed())
			Expect(order).To(BeEmpty())
		})

		It("should build task names from non-empty parts using the configured delimiter", func() {
			fixture := plumbertests.NewPlumber()
			fixture.Plumber.SetDelimiter("/")

			task := fixture.NewTaskList("tasks").CreateTask("deploy", "", "prepare")

			Expect(task.Name).To(Equal("deploy/prepare"))
		})
	})

	Describe("task lists", func() {
		It("should run task list hooks and jobs", func() {
			fixture := plumbertests.NewPlumber()
			tl := fixture.NewTaskList("list")
			order := []string{}

			tl.
				ShouldRunBefore(func(_ *plumber.TaskList) error {
					order = append(order, "before")

					return nil
				}).
				Set(func(_ *plumber.TaskList) plumber.Job {
					return plumber.CreateBasicJob(func() error {
						order = append(order, "run")

						return nil
					})
				}).
				ShouldRunAfter(func(_ *plumber.TaskList) error {
					order = append(order, "after")

					return nil
				})

			Expect(tl.RunBefore()).To(Succeed())
			Expect(tl.Run()).To(Succeed())
			Expect(tl.RunAfter()).To(Succeed())
			Expect(order).To(Equal([]string{"before", "run", "after"}))
		})
	})

	Describe("commands", func() {
		var task *plumber.Task

		BeforeEach(func() {
			fixture := plumbertests.NewPlumber()
			task = fixture.NewTaskList("commands").CreateTask("command")
		})

		It("should format commands with a shell prompt prefix", func() {
			command := task.CreateCommand("echo", "hello")

			Expect(command.GetFormattedCommand()).To(Equal("$ echo hello"))
		})

		It("should record stdout, stderr, and combined streams", func() {
			command := task.
				CreateCommand("sh", "-c", `sleep 0.05; printf 'out\n'; printf 'err\n' >&2`).
				EnableStreamRecording()

			Expect(command.Run()).To(Succeed())
			Eventually(command.GetStdoutStream).Should(Equal([]string{"out\n"}))
			Eventually(command.GetStderrStream).Should(Equal([]string{"err\n"}))
			Eventually(command.GetCombinedStream).Should(ConsistOf("out\n", "err\n"))
		})

		It("should use explicitly appended environment when OS environment is masked", func() {
			plumbertests.WithEnvironment(map[string]string{
				"PLUMBER_TEST_COMMAND_ENV": "from-os",
			})

			command := task.
				CreateCommand("sh", "-c", `printf '%s\n' "${PLUMBER_TEST_COMMAND_ENV:-missing}"`).
				SetMaskOsEnvironment().
				AppendEnvironment(map[string]string{
					"PLUMBER_TEST_COMMAND_ENV": "from-command",
				}).
				EnableStreamRecording()

			Expect(command.Run()).To(Succeed())
			Eventually(command.GetStdoutStream).Should(Equal([]string{"from-command\n"}))
		})

		It("should template inline scripts into stdin", func() {
			command := task.
				CreateCommand("cat").
				SetScript(func(_ *plumber.Command) *plumber.CommandScript {
					return &plumber.CommandScript{
						Inline: "hello {{ .Name }}\n",
						Ctx: map[string]string{
							"Name": "plumber",
						},
					}
				}).
				EnableStreamRecording()

			Expect(command.Run()).To(Succeed())
			Eventually(command.GetStdoutStream).Should(Equal([]string{"hello plumber\n"}))
		})

		It("should stream custom stdin when no script is configured", func() {
			command := task.
				CreateCommand("cat").
				SetStdin(func(_ *plumber.Command) io.Reader {
					return strings.NewReader("from stdin\n")
				}).
				EnableStreamRecording()

			Expect(command.Run()).To(Succeed())
			Eventually(command.GetStdoutStream).Should(Equal([]string{"from stdin\n"}))
		})

		It("should ignore command errors when configured", func() {
			command := task.
				CreateCommand("sh", "-c", "exit 7").
				SetIgnoreError()

			Expect(command.Run()).To(Succeed())
		})

		It("should retry failed commands until tries are exhausted", func() {
			retry := &plumber.CommandRetry{
				Tries: 1,
				Delay: time.Millisecond,
			}
			command := task.
				CreateCommand("sh", "-c", "exit 7").
				SetRetries(retry)

			Expect(command.Run()).To(HaveOccurred())
			Expect(retry.Tries).To(BeEquivalentTo(0))
		})

		It("should run command hooks around the process", func() {
			command := task.CreateCommand("true")
			order := []string{}

			command.
				ShouldRunBefore(func(_ *plumber.Command) error {
					order = append(order, "before")

					return nil
				}).
				ShouldRunAfter(func(_ *plumber.Command) error {
					order = append(order, "after")

					return nil
				}).
				Set(func(c *plumber.Command) error {
					order = append(order, fmt.Sprintf("set:%s", c.GetFormattedCommand()))

					return nil
				})

			Expect(command.Run()).To(Succeed())
			Expect(order).To(Equal([]string{"set:$ true", "before", "after"}))
		})

		It("should skip disabled commands without executing hooks", func() {
			command := task.CreateCommand("false")
			order := []string{}

			command.
				ShouldDisable(func(_ *plumber.Task) bool {
					return true
				}).
				ShouldRunBefore(func(_ *plumber.Command) error {
					order = append(order, "before")

					return nil
				})

			Expect(command.Run()).To(Succeed())
			Expect(order).To(BeEmpty())
		})

		It("should remove empty command arguments before execution", func() {
			command := task.
				CreateCommand("sh", "", "-c", "", `printf 'ok\n'`).
				EnableStreamRecording()

			Expect(command.Run()).To(Succeed())
			Eventually(command.GetStdoutStream).Should(Equal([]string{"ok\n"}))
			Expect(command.GetFormattedCommand()).To(Equal("$ sh -c printf 'ok\\n'"))
		})

		It("should return an error when a script has no source", func() {
			command := task.
				CreateCommand("cat").
				SetScript(func(_ *plumber.Command) *plumber.CommandScript {
					return &plumber.CommandScript{}
				})

			Expect(command.Run()).To(MatchError("Either file or inline has to be set for command script."))
		})
	})

	Describe("process args", func() {
		It("should expose temporary args to specs", func() {
			plumbertests.WithArgs("plumber", "run", "--debug")

			Expect(os.Args).To(Equal([]string{"plumber", "run", "--debug"}))
		})
	})
})
