package plumber_test

import (
	"context"
	"errors"
	"time"

	"github.com/cenk1cenk2/plumber/v6"
	plumbertests "github.com/cenk1cenk2/plumber/v6/tests"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("plumber lifecycle", func() {
	BeforeEach(func() {
		plumbertests.WithoutEnvironment("CI", "CLI_ARGS", "DEBUG", "ENV_FILE")
	})

	Describe("Cli parsing and setup", func() {
		It("should append CLI_ARGS after process args so command flags parse", func() {
			plumbertests.WithEnvironment(map[string]string{
				"CLI_ARGS": "--name from-cli-args",
			})
			plumbertests.WithArgs("cli-args-test", "run")
			parsedName := ""
			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "cli-args-test",
					Commands: []*cli.Command{
						{
							Name: "run",
							Flags: []cli.Flag{
								&cli.StringFlag{Name: "name"},
							},
							Action: func(_ context.Context, command *cli.Command) error {
								parsedName = command.String("name")

								return nil
							},
						},
					},
				}
			})

			fixture.Plumber.Run()

			Expect(parsedName).To(Equal("from-cli-args"))
		})

		It("should parse root flags and environment-backed flags before actions", func() {
			plumbertests.WithArgs("setup-test", "--ci", "--log-level", "warn", "--debug", "run")
			var debug bool
			var ci bool
			var level logrus.Level
			fixture := plumbertests.NewPlumber(func(app *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "setup-test",
					Commands: []*cli.Command{
						{
							Name: "run",
							Action: func(_ context.Context, _ *cli.Command) error {
								debug = app.Environment.Debug
								ci = app.Environment.CI
								level = app.Log.GetLevel()

								return nil
							},
						},
					},
				}
			})

			fixture.Plumber.Run()

			Expect(debug).To(BeTrue())
			Expect(ci).To(BeTrue())
			Expect(level).To(Equal(logrus.DebugLevel))
		})

		It("should run the wrapped Cli Before hook before actions", func() {
			plumbertests.WithArgs("before-test", "run")
			order := []string{}
			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "before-test",
					Before: func(ctx context.Context, _ *cli.Command) (context.Context, error) {
						order = append(order, "before")

						return ctx, nil
					},
					Commands: []*cli.Command{
						{
							Name: "run",
							Action: func(_ context.Context, _ *cli.Command) error {
								order = append(order, "action")

								return nil
							},
						},
					},
				}
			})

			fixture.Plumber.Run()

			Expect(order).To(Equal([]string{"before", "action"}))
		})

		It("should run configured greeters before Cli actions", func() {
			plumbertests.WithArgs("greeter-test", "run")
			order := []string{}
			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "greeter-test",
					Commands: []*cli.Command{
						{
							Name: "run",
							Action: func(_ context.Context, _ *cli.Command) error {
								order = append(order, "action")

								return nil
							},
						},
					},
				}
			})
			fixture.Plumber.SetGreeter(func(_ *plumber.Plumber) error {
				order = append(order, "greeter")

				return nil
			})

			fixture.Plumber.Run()

			Expect(order).To(Equal([]string{"greeter", "action"}))
		})

		It("should allow non-fatal deprecation notices for parsed flags", func() {
			plumbertests.WithArgs("deprecation-flag-test", "--old-mode", "run")
			ran := false
			fixture := plumbertests.NewPlumber(func(_ *plumber.Plumber) *cli.Command {
				return &cli.Command{
					Name: "deprecation-flag-test",
					Flags: []cli.Flag{
						&cli.BoolFlag{Name: "old-mode"},
					},
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
					Flag:  []string{"--old-mode"},
					Level: plumber.LOG_LEVEL_WARN,
				},
			})

			fixture.Plumber.Run()

			Expect(ran).To(BeTrue())
		})
	})

	Describe("configuration and jobs", func() {
		It("should apply Set hooks and expose the configured delimiter to new tasks", func() {
			fixture := plumbertests.NewPlumber()

			result := fixture.Plumber.Set(func(app *plumber.Plumber) error {
				app.SetDelimiter("|")
				app.SetTerminatorTimeout(time.Millisecond)

				return nil
			})

			Expect(result).To(BeIdenticalTo(fixture.Plumber))
			Expect(fixture.NewTaskList("configured").CreateTask("deploy", "", "prepare").Name).To(Equal("deploy|prepare"))
		})

		It("should accept empty runtimes by restoring the default runtime", func() {
			fixture := plumbertests.NewPlumber()

			Expect(fixture.Plumber.SetRuntime(plumber.Runtime{})).To(BeIdenticalTo(fixture.Plumber))
		})

		DescribeTable("should run jobs through plumber",
			func(job plumber.Job, expected error) {
				fixture := plumbertests.NewPlumber()

				err := fixture.Plumber.RunJobs(job)
				if expected == nil {
					Expect(err).ToNot(HaveOccurred())

					return
				}

				Expect(err).To(MatchError(expected))
			},
			Entry("nil jobs", nil, nil),
			Entry("successful jobs", plumber.CreateBasicJob(func() error {
				return nil
			}), nil),
			Entry("failed jobs", plumber.CreateBasicJob(func() error {
				return errors.New("job failed")
			}), errors.New("job failed")),
		)
	})
})
