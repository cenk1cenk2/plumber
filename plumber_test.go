package plumber_test

import (
	"bytes"
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
		It("should create a Cli application with default flags", func() {
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
			output := &bytes.Buffer{}
			fixture.Plumber.Log.SetOutput(io.MultiWriter(GinkgoWriter, output))

			fixture.Plumber.AppendSecrets("secret-token")
			fixture.Plumber.Log.Info("using secret-token")

			Expect(output.String()).To(ContainSubstring("[REDACTED]"))
			Expect(output.String()).ToNot(ContainSubstring("secret-token"))
		})
	})

	Describe("validation", func() {
		type defaultedConfig struct {
			Name string `default:"worker" validate:"required"`
		}

		type invalidConfig struct {
			Name string `validate:"required"`
		}

		type validationCase struct {
			build  func() any
			assert func(error, any)
		}

		DescribeTable("should validate data",
			func(tc validationCase) {
				fixture := plumbertests.NewPlumber()
				config := tc.build()

				tc.assert(fixture.Plumber.Validate(config), config)
			},
			Entry("apply defaults before validating", validationCase{
				build: func() any {
					return &defaultedConfig{}
				},
				assert: func(err error, config any) {
					Expect(err).ToNot(HaveOccurred())
					Expect(config.(*defaultedConfig).Name).To(Equal("worker"))
				},
			}),
			Entry("return the current validation error message", validationCase{
				build: func() any {
					return &invalidConfig{}
				},
				assert: func(err error, _ any) {
					Expect(err).To(MatchError("Validation failed."))
				},
			}),
		)
	})

	Describe("templates", func() {
		type inlineTemplateCase struct {
			template string
			ctx      any
			funcs    []template.FuncMap
			expected string
		}

		DescribeTable("should execute inline templates",
			func(tc inlineTemplateCase) {
				result, err := plumber.InlineTemplate(tc.template, tc.ctx, tc.funcs...)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(tc.expected))
			},
			Entry("slim sprig and custom template functions", inlineTemplateCase{
				template: `{{ .Name | upper }} {{ shout .Suffix }}`,
				ctx: map[string]string{
					"Name":   "plumber",
					"Suffix": "tests",
				},
				funcs: []template.FuncMap{
					{
						"shout": strings.ToUpper,
					},
				},
				expected: "PLUMBER TESTS",
			}),
			Entry("empty templates", inlineTemplateCase{
				template: "",
				ctx:      map[string]string{},
				expected: "",
			}),
		)

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

	Describe("Cli flag and argument helpers", func() {
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
		newTask := func() *plumber.Task {
			GinkgoHelper()

			fixture := plumbertests.NewPlumber()

			return fixture.NewTaskList("commands").CreateTask("command")
		}
		newTaskWithRunner := func() (*plumber.Task, *plumbertests.TestingCommandRunner) {
			GinkgoHelper()

			fixture := plumbertests.NewPlumber()
			runner := plumbertests.NewTestingCommandRunner()
			fixture.Plumber.SetRuntime(plumber.Runtime{CommandRunner: runner.Runner()})

			return fixture.NewTaskList("commands").CreateTask("command"), runner
		}

		It("should format commands with a shell prompt prefix", func() {
			command := newTask().CreateCommand("echo", "hello")

			Expect(command.GetFormattedCommand()).To(Equal("$ echo hello"))
		})

		It("should record stdout, stderr, and combined streams", func() {
			task, runner := newTaskWithRunner()
			runner.Add(plumbertests.TestingCommandResponse{
				Stdout: "out\n",
				Stderr: "err\n",
			})
			command := task.
				CreateCommand("mock").
				EnableStreamRecording()

			Expect(command.Run()).To(Succeed())
			Expect(command.GetStdoutStream()).To(Equal([]string{"out\n"}))
			Expect(command.GetStderrStream()).To(Equal([]string{"err\n"}))
			Expect(command.GetCombinedStream()).To(ConsistOf("out\n", "err\n"))
		})

		It("should use explicitly appended environment when OS environment is masked", func() {
			task, runner := newTaskWithRunner()
			plumbertests.WithEnvironment(map[string]string{
				"PLUMBER_TEST_COMMAND_ENV": "from-os",
			})

			command := task.
				CreateCommand("mock").
				SetMaskOsEnvironment().
				AppendEnvironment(map[string]string{
					"PLUMBER_TEST_COMMAND_ENV": "from-command",
				})

			Expect(command.Run()).To(Succeed())
			Expect(runner.Invocations()).To(HaveLen(1))
			Expect(runner.Invocations()[0].Env).To(Equal([]string{"PLUMBER_TEST_COMMAND_ENV=from-command"}))
		})

		DescribeTable("should pass stdin to command invocations",
			func(configure func(*plumber.Task) *plumber.Command, expected string) {
				task, runner := newTaskWithRunner()
				command := configure(task)

				Expect(command.Run()).To(Succeed())
				Expect(runner.Invocations()).To(HaveLen(1))
				stdin, err := plumbertests.ReadInvocationStdin(runner.Invocations()[0])
				Expect(err).ToNot(HaveOccurred())
				Expect(stdin).To(Equal(expected))
			},
			Entry("inline scripts", func(task *plumber.Task) *plumber.Command {
				return task.
					CreateCommand("cat").
					SetScript(func(_ *plumber.Command) *plumber.CommandScript {
						return &plumber.CommandScript{
							Inline: "hello {{ .Name }}\n",
							Ctx: map[string]string{
								"Name": "plumber",
							},
						}
					})
			}, "hello plumber\n"),
			Entry("custom stdin", func(task *plumber.Task) *plumber.Command {
				return task.
					CreateCommand("cat").
					SetStdin(func(_ *plumber.Command) io.Reader {
						return strings.NewReader("from stdin\n")
					})
			}, "from stdin\n"),
		)

		It("should ignore command errors when configured", func() {
			task, runner := newTaskWithRunner()
			result := plumbertests.TestingCommandFailure(7)
			runner.Add(plumbertests.TestingCommandResponse{
				Result: &result,
			})
			command := task.
				CreateCommand("mock").
				SetIgnoreError()

			Expect(command.Run()).To(Succeed())
		})

		It("should retry failed commands until tries are exhausted", func() {
			task, runner := newTaskWithRunner()
			result := plumbertests.TestingCommandFailure(7)
			runner.
				Add(plumbertests.TestingCommandResponse{Result: &result}).
				Add(plumbertests.TestingCommandResponse{Result: &result})
			retry := &plumber.CommandRetry{
				Tries: 1,
				Delay: time.Millisecond,
			}
			command := task.
				CreateCommand("mock").
				SetRetries(retry)

			Expect(command.Run()).To(HaveOccurred())
			Expect(retry.Tries).To(BeEquivalentTo(0))
			Expect(runner.Invocations()).To(HaveLen(2))
		})

		It("should run command hooks around the process", func() {
			task, _ := newTaskWithRunner()
			command := task.CreateCommand("mock")
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
			Expect(order).To(Equal([]string{"set:$ mock", "before", "after"}))
		})

		It("should skip disabled commands without executing hooks", func() {
			task, runner := newTaskWithRunner()
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
			Expect(runner.Invocations()).To(BeEmpty())
		})

		It("should remove empty command arguments before execution", func() {
			task, runner := newTaskWithRunner()
			command := task.
				CreateCommand("mock", "", "-c", "", `printf 'ok\n'`)

			Expect(command.Run()).To(Succeed())
			Expect(runner.Invocations()[0].Args).To(Equal([]string{"-c", `printf 'ok\n'`}))
			Expect(command.GetFormattedCommand()).To(Equal("$ mock -c printf 'ok\\n'"))
		})

		It("should return an error when a script has no source", func() {
			command := newTask().
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
