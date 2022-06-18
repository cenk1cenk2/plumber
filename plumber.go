package plumber

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gitlab.kilic.dev/libraries/go-utils/logger"
)

type Plumber struct {
	Cli         *cli.App
	Log         *logrus.Logger
	Environment AppEnvironment
	Channel     AppChannel

	onTerminateFn
	readme string
}

type AppEnvironment struct {
	Debug bool
	CI    bool
}

type AppChannel struct {
	// to communicate the errors while not blocking
	Err chan error
	// Fatal errors
	Fatal chan error
	// terminate channel
	Interrupt chan os.Signal
	// exit channel
	Exit chan int
}

type (
	onTerminateFn func() error
)

// Cli.New Creates a new plumber for pipes.
func (p *Plumber) New(
	fn func(a *Plumber) *cli.App,
) *Plumber {
	p.Cli = fn(p)

	// have to this here too, to catch cli errors, but it is singleton so not much harm
	p.Log = logger.InitiateLogger(logrus.InfoLevel)
	p.setupLogger(logrus.InfoLevel)

	p.Cli.Before = p.setup(p.Cli.Before)

	if p.Cli.Action == nil {
		p.Cli.Action = p.defaultAction()
	}

	p.Cli.Flags = p.appendDefaultFlags(p.Cli.Flags)

	p.Cli.Commands = append(p.Cli.Commands, &cli.Command{
		Name: "docs",
		Action: func(ctx *cli.Context) error {
			return p.generateMarkdownDocumentation()
		},
		Hidden:   true,
		HideHelp: true,
	})

	p.readme = "README.md"

	if len(p.Cli.Commands) > 0 {
		for i, v := range p.Cli.Commands {
			p.Cli.Commands[i].Flags = p.appendDefaultFlags(v.Flags)
		}
	}

	p.Environment = AppEnvironment{}

	// create error channels
	p.Channel = AppChannel{
		Err:       make(chan error),
		Fatal:     make(chan error),
		Interrupt: make(chan os.Signal),
		Exit:      make(chan int, 1),
	}

	return p
}

// Cli.Run Starts the application.
func (p *Plumber) Run() {
	p.greet()

	go p.registerHandlers()

	if err := p.Cli.Run(os.Args); err != nil {
		p.Channel.Fatal <- err

		for {
			// to be sure that os.exit is completed
			<-p.Channel.Exit
		}
	}
}

// Cli.AppendFlags Appends flags together.
func (p *Plumber) AppendFlags(flags ...[]cli.Flag) []cli.Flag {
	f := []cli.Flag{}

	for _, v := range flags {
		f = append(f, v...)
	}

	return f
}

// Cli.SetOnTerminate Sets the action that would be executed on terminate.
func (p *Plumber) SetOnTerminate(fn onTerminateFn) *Plumber {
	p.onTerminateFn = fn

	return p
}

// Cli.SetReadme Sets readme file for documentation generation.
func (p *Plumber) SetReadme(file string) *Plumber {
	p.readme = file

	return p
}

// Cli.greet Greet the user with the application name and version.
func (p *Plumber) greet() {
	var version = p.Cli.Version

	if version == "latest" {
		version = fmt.Sprintf("BUILD.%s", p.Cli.Compiled.UTC().Format("20060102-1504"))
	}

	name := fmt.Sprintf("%s - %s", p.Cli.Name, version)
	fmt.Println(name)
	fmt.Println(strings.Repeat("-", len(name)))
}

func (p *Plumber) appendDefaultFlags(flags []cli.Flag) []cli.Flag {
	f := []cli.Flag{}

	f = append(f, CliDefaultFlags...)
	f = append(f, flags...)

	return f
}

// Cli.loadEnvironment Loads the given environment file to the application.
func (p *Plumber) loadEnvironment() error {
	if env := os.Getenv("ENV_FILE"); env != "" {
		if err := godotenv.Load(env); err != nil {
			return err
		}
	}

	return nil
}

// Cli.setup Before function for the CLI that gets executed setup the action.
func (p *Plumber) setup(before cli.BeforeFunc) cli.BeforeFunc {
	return func(ctx *cli.Context) error {
		if err := p.loadEnvironment(); err != nil {
			return err
		}

		level, err := logrus.ParseLevel(ctx.String("log-level"))

		if err != nil {
			level = logrus.InfoLevel
		}

		if ctx.Bool("debug") {
			level = logrus.DebugLevel

			p.Environment.Debug = true
		}

		if ctx.Bool("ci") {
			p.Environment.CI = true
		}

		p.setupLogger(level)

		if before != nil {
			if err := before(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

func (p *Plumber) setupLogger(level LogLevel) {
	p.Log.Level = level

	p.Log.SetFormatter(
		&logger.Formatter{
			FieldsOrder:      []string{"context", "status"},
			TimestampFormat:  "",
			HideKeys:         true,
			NoColors:         false,
			NoFieldsColors:   false,
			NoFieldsSpace:    false,
			ShowFullLevel:    false,
			NoUppercaseLevel: false,
			TrimMessages:     true,
			CallerFirst:      false,
		},
	)

	p.Log.ExitFunc = p.Terminate
}

func (p *Plumber) defaultAction() cli.ActionFunc {
	return func(ctx *cli.Context) error {
		if err := cli.ShowAppHelp(ctx); err != nil {
			return err
		}

		return fmt.Errorf("Application needs a subcommand to run.")
	}
}

// App.registerInterruptHandler Registers the os.Signal listener for the application.
func (p *Plumber) registerInterruptHandler() {
	signal.Notify(p.Channel.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	interrupt := <-p.Channel.Interrupt
	p.Log.Errorf(
		"Terminating the application with signal: %s",
		interrupt,
	)

	p.Terminate(1)
}

func (p *Plumber) registerHandlers() {
	go p.registerErrorHandler()
	go p.registerFatalErrorHandler()
	go p.registerInterruptHandler()
	go p.registerExitHandler()
}

// App.registerErrorHandler Registers the error handlers for the runtime errors, this will not terminate application.
func (p *Plumber) registerErrorHandler() {
	for {
		err := <-p.Channel.Err

		if err == nil {
			return
		}

		p.Log.Errorln(err)
	}
}

// App.registerFatalErrorHandler Registers the error handler for fatal errors, this will terminate the application.
func (p *Plumber) registerFatalErrorHandler() {
	for {
		err := <-p.Channel.Fatal

		if err == nil {
			return
		}

		if p.Log != nil {
			p.Log.Fatalln(err)
		} else {
			panic(err)
		}
	}
}

func (p *Plumber) registerExitHandler() {
	os.Exit(<-p.Channel.Exit)
}

// App.Terminate Terminates the application.
func (p *Plumber) Terminate(code int) {
	if p.onTerminateFn != nil {
		p.Channel.Err <- p.onTerminateFn()
	}

	close(p.Channel.Err)
	close(p.Channel.Fatal)
	close(p.Channel.Interrupt)

	p.Channel.Exit <- code
}

func (p *Plumber) generateMarkdownDocumentation() error {
	const start = "<!-- clidocs -->"
	const end = "<!-- clidocsstop -->"
	expr := fmt.Sprintf(`(?s)%s(.*)%s`, start, end)

	p.Log.Debugf("Using expression: %s", expr)

	data, err := p.Cli.ToMarkdown()

	if err != nil {
		return err
	}

	p.Log.Infof("Trying to read file: %s", p.readme)

	content, err := ioutil.ReadFile(p.readme)

	if err != nil {
		return err
	}

	readme := string(content)

	r := regexp.MustCompile(expr)

	replace := strings.Join([]string{start, "", data, end}, "\n")

	result := r.ReplaceAllString(readme, replace)

	f, err := os.OpenFile(p.readme,
		os.O_WRONLY, 0644)

	if err != nil {
		return err
	}

	defer f.Close()
	if _, err := f.WriteString(result); err != nil {
		return err
	}

	p.Log.Infof("Wrote to file: %s", p.readme)

	return nil
}
