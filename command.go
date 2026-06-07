package plumber

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/sirupsen/logrus"
)

type Command struct {
	Plumber *Plumber
	T       *Task
	TL      *TaskList
	Log     *logrus.Entry

	Command  *exec.Cmd
	scriptFn CommandScriptFn
	options  CommandOptions
	runtime  Runtime

	shouldRunBeforeFn CommandFn
	fn                CommandFn
	shouldRunAfterFn  CommandFn
	onTerminatorFn    CommandFn
	jobWrapperFn      CommandJobWrapperFn
	credentialFn      CommandCredentialFn

	stdoutLevel   LogLevel
	stderrLevel   LogLevel
	lifetimeLevel LogLevel

	stdinFn        CommandStdinFn
	stdoutStream   []string
	stderrStream   []string
	combinedStream []string
	lockStream     *sync.RWMutex

	status CommandStatus
}

type CommandOptions struct {
	disablePredicateFn TaskPredicateFn
	ignoreError        bool
	recordStream       bool
	ensureIsAlive      bool
	maskOsEnvironment  bool
	retry              *CommandRetry
}

type CommandStatus struct {
	stopCases StatusStopCases
	result    CommandResult
	resultSet bool
}

type CommandScript struct {
	Inline string
	File   string
	Ctx    interface{}
	Funcs  []template.FuncMap
}

type CommandRetry struct {
	Tries  uint32
	Always bool
	Delay  time.Duration
}

type (
	CommandFn           func(*Command) error
	CommandJobWrapperFn func(job Job, c *Command) Job
	CommandStdinFn      func(c *Command) io.Reader
	CommandScriptFn     func(c *Command) *CommandScript
	CommandCredentialFn func(c *Command, credential *syscall.Credential) *syscall.Credential
)

type commandStreamWriter struct {
	command *Command
	level   LogLevel
	stream  string
	buffer  string
}

const (
	stream_stdout       string        = "stdout"
	stream_stderr       string        = "stderr"
	COMMAND_RETRY_DELAY time.Duration = time.Second
)

// NewCommand Creates a new command to be run as a job.
func NewCommand(
	task *Task,
	command string,
	args ...string,
) *Command {
	c := &Command{
		Command: exec.Command(command, args...),
		Plumber: task.Plumber,
		T:       task,
		TL:      task.TL,
		Log:     task.Log,
	}

	c.Command.SysProcAttr = &syscall.SysProcAttr{}

	c.SetLogLevel(LOG_LEVEL_DEFAULT, LOG_LEVEL_DEFAULT, LOG_LEVEL_DEFAULT)

	return c
}

// Sets the function that should manipulate the command depending on the pipe variables.
func (c *Command) Set(fn CommandFn) *Command {
	c.fn = fn

	return c
}

// Sets a function that should run after the main command has exited successfully.
func (c *Command) ShouldRunBefore(fn CommandFn) *Command {
	c.shouldRunBeforeFn = fn

	return c
}

// Sets a function that should run after the main command has exited successfully.
func (c *Command) ShouldRunAfter(fn CommandFn) *Command {
	c.shouldRunAfterFn = fn

	return c
}

// Checks whether current command is disabled.
func (c *Command) IsDisabled() bool {
	if c.options.disablePredicateFn == nil {
		return false
	}

	return c.options.disablePredicateFn(c.T)
}

// Adds a predicate to check whether this command should be disabled depending on the pipe variables.
func (c *Command) ShouldDisable(fn TaskPredicateFn) *Command {
	c.options.disablePredicateFn = fn

	return c
}

// Enables global plumber terminator on this command to terminate the current command when the application is terminated.
func (c *Command) EnableTerminator() *Command {
	c.Log.Tracef("Registered terminator: %s", c.GetFormattedCommand())
	c.Plumber.RegisterTerminator()

	go c.handleTerminator()

	return c
}

// Sets a function that fires on when the application is globally terminated through plumber.
func (c *Command) SetOnTerminator(fn CommandFn) *Command {
	c.onTerminatorFn = fn

	return c
}

func (c *Command) SetScript(fn CommandScriptFn) *Command {
	c.scriptFn = fn

	return c
}

func (c *Command) SetCredential(fn CommandCredentialFn) *Command {
	c.credentialFn = fn

	return c
}

func (c *Command) SetStdin(fn CommandStdinFn) *Command {
	c.stdinFn = fn

	return c
}

// Appends arguments to the command.
func (c *Command) AppendArgs(args ...string) *Command {
	c.Command.Args = append(
		c.Command.Args,
		args...,
	)

	return c
}

// Appends environment variables to command as map.
func (c *Command) AppendEnvironment(environment map[string]string) *Command {
	for k, v := range environment {
		c.AppendDirectEnvironment(fmt.Sprintf("%s=%s", k, v))
	}

	return c
}

// Appends environment variables to command directly.
func (c *Command) AppendDirectEnvironment(environment ...string) *Command {
	c.Command.Env = append(c.Command.Env, environment...)

	return c
}

// Sets the log level specific to this command.
func (c *Command) SetLogLevel(
	stdout LogLevel,
	stderr LogLevel,
	lifetime LogLevel,
) *Command {
	if stdout == 0 {
		c.stdoutLevel = logrus.InfoLevel
	} else {
		c.stdoutLevel = stdout
	}

	if stderr == 0 {
		c.stderrLevel = logrus.WarnLevel
	} else {
		c.stderrLevel = stderr
	}

	if lifetime == 0 {
		c.lifetimeLevel = logrus.InfoLevel
	} else {
		c.lifetimeLevel = lifetime
	}

	return c
}

// Sets the current directory where the command will be executed.
func (c *Command) SetDir(dir string) *Command {
	c.Command.Dir = dir

	return c
}

// Sets the current directory where the command will be executed.
func (c *Command) SetPath(dir string) *Command {
	c.Command.Path = dir

	return c
}

// Sets the option to ignore errors raised by this command therefore a failing command will not fail the application.
func (c *Command) SetIgnoreError() *Command {
	c.options.ignoreError = true

	return c
}

// Sets the option where underlying environment variables are not passed to the command.
func (c *Command) SetMaskOsEnvironment() *Command {
	c.options.maskOsEnvironment = true

	return c
}

// Sets the option to retry the command if failed.
func (c *Command) SetRetries(retry *CommandRetry) *Command {
	c.options.retry = retry

	return c
}

// Extend the job of the current task.
func (c *Command) SetJobWrapper(fn CommandJobWrapperFn) *Command {
	c.jobWrapperFn = fn

	return c
}

func (c *Command) SetRuntime(runtime Runtime) *Command {
	c.runtime = runtime

	return c
}

// Sets the option where this command will save its output to be later accessed in the shouldRunAfterFn.
func (c *Command) EnableStreamRecording() *Command {
	c.options.recordStream = true
	c.lockStream = &sync.RWMutex{}

	return c
}

// Sets the option where it will raise an error if the underlying command stops.
func (c *Command) EnsureIsAlive() *Command {
	c.options.ensureIsAlive = true

	return c
}

// Fetches the saved stdout stream that is recorded.
// Should have the Command.options.recordStream enabled.
func (c *Command) GetStdoutStream() []string {
	if !c.options.recordStream {
		c.T.SendFatal(
			fmt.Errorf("Stream recording should be enabled to fetch the command output stream."),
		)
	}

	c.lockStream.Lock()
	stream := c.stdoutStream
	c.lockStream.Unlock()

	return stream
}

// Fetches the saved stderr stream that is recorded.
// Should have the Command.options.recordStream enabled.
func (c *Command) GetStderrStream() []string {
	if !c.options.recordStream {
		c.T.SendFatal(
			fmt.Errorf("Stream recording should be enabled to fetch the command output stream."),
		)
	}

	c.lockStream.Lock()
	stream := c.stderrStream
	c.lockStream.Unlock()

	return stream
}

// Fetches the saved streams that is recorded.
// Should have the Command.options.recordStream enabled.
func (c *Command) GetCombinedStream() []string {
	if !c.options.recordStream {
		c.T.SendFatal(
			fmt.Errorf("Stream recording should be enabled to fetch the command output stream."),
		)
	}

	c.lockStream.Lock()
	stream := c.combinedStream
	c.lockStream.Unlock()

	return stream
}

// Returns whether the command has failed or not.
func (c *Command) HasFailed() bool {
	if c.status.resultSet {
		return !c.status.result.Success
	}

	if c.Command.ProcessState == nil {
		return false
	}

	return !c.Command.ProcessState.Success()
}

// Returns whether the command has exited properly or not.
func (c *Command) HasExited() bool {
	if c.status.resultSet {
		return !c.status.result.Exited
	}

	if c.Command.ProcessState == nil {
		return false
	}

	return !c.Command.ProcessState.Exited()
}

// Fetches the name of this command, that is formatted for the logger.
func (c *Command) GetFormattedCommand() string {
	return fmt.Sprintf("$ %s", strings.Join(c.Command.Args, " "))
}

// Run the command as defined.
func (c *Command) Run() error {
	return c.run(Runtime{})
}

func (c *Command) run(runtime Runtime) error {
	if stop := c.handleStopCases(); stop {
		return nil
	}

	started := time.Now()
	if c.fn != nil {
		if err := c.fn(c); err != nil {
			return err
		}
	}

	c.Command.Args = slices.DeleteFunc(c.Command.Args, func(arg string) bool {
		return arg == ""
	})

	if !c.options.maskOsEnvironment {
		c.Command.Env = append(c.Command.Env, os.Environ()...)
	}

	c.Log.WithField(LOG_FIELD_STATUS, log_status_run).
		Log(c.lifetimeLevel, c.GetFormattedCommand())

	if c.shouldRunBeforeFn != nil {
		if err := c.shouldRunBeforeFn(c); err != nil {
			return err
		}
	}

	if err := c.pipe(runtime); err != nil {
		c.Log.WithField(LOG_FIELD_STATUS, log_status_fail).
			Errorf("%s > %s", c.GetFormattedCommand(), err.Error())

		return err
	}

	if c.shouldRunAfterFn != nil {
		if err := c.shouldRunAfterFn(c); err != nil {
			return err
		}
	}

	c.Log.WithField(LOG_FIELD_STATUS, log_status_end).
		Logf(c.lifetimeLevel, "%s -> %s", c.GetFormattedCommand(), time.Since(started).Round(time.Millisecond).String())

	return nil
}

func (c *Command) RunWith(runtime Runtime) error {
	return c.run(runtime)
}

// Convert Command.Run to a floc job.
func (c *Command) Job() Job {
	return JobIfNot(
		Predicate(func() bool {
			return c.handleStopCases()
		}),
		CreateJob(func() error {
			if c.jobWrapperFn != nil {
				return c.Plumber.RunJobs(c.jobWrapperFn(
					CreateBasicJob(c.Run),
					c,
				))
			}

			return c.Run()
		}),
		CreateJob(func() error {
			return nil
		}),
	)
}

// Adds the current command to the parent task.
func (c *Command) AddSelfToTheTask() *Command {
	c.T.AddCommands(c)

	return c
}

// Adds the current command to the task with a wrapper.
func (c *Command) AddSelfToTheParentTask(pt *Task) *Command {
	pt.AddCommands(c)

	return c
}

// Executes the command and pipes the output through the logger.
func (c *Command) pipe(runtime Runtime) error {
	invocation, err := c.createInvocation()
	if err != nil {
		return err
	}

	c.resetStreams()

	result, err := c.resolveCommandRunner(runtime).Run(c.Plumber.context, invocation, CommandRuntime{
		Stdout: c.newStreamWriter(stream_stdout, c.stdoutLevel),
		Stderr: c.newStreamWriter(stream_stderr, c.stderrLevel),
		SetProcess: func(process *os.Process) {
			c.Command.Process = process
		},
	})
	c.Command.Process = nil
	c.status.result = result
	c.status.resultSet = result.Started || result.ProcessState != nil
	c.Command.ProcessState = result.ProcessState

	if err != nil {
		if result.Started {
			if exiterr, ok := err.(*exec.ExitError); ok {
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					c.Log.WithField(LOG_FIELD_STATUS, log_status_exit).
						Debugf("%s > Exit Code: %v", c.GetFormattedCommand(), status.ExitStatus())
				}
			}

			return c.retry(err, runtime)
		}

		c.Log.WithField(LOG_FIELD_STATUS, log_status_fail).
			Debugf("%s > Can not start command!", c.GetFormattedCommand())

		return err
	}

	if !result.Success || result.ExitCode != 0 {
		err := &commandResultError{
			command:  c.GetFormattedCommand(),
			exitCode: result.ExitCode,
		}
		c.Log.WithField(LOG_FIELD_STATUS, log_status_exit).
			Debugf("%s > Exit Code: %v", c.GetFormattedCommand(), result.ExitCode)

		return c.retry(err, runtime)
	}

	if c.options.ensureIsAlive {
		return fmt.Errorf("Process not running anymore: %s", c.GetFormattedCommand())
	}

	return nil
}

// Handles the error depending on the options.
func (c *Command) handleError(err error) error {
	if c.options.ignoreError {
		c.Log.Debugf("%s -> Error ignored: %s", c.GetFormattedCommand(), err.Error())

		return nil
	}

	return err
}

// Retries the task with the given options.
func (c *Command) retry(err error, runtime Runtime) error {
	if c.options.retry == nil || !c.options.retry.Always && c.options.retry.Tries <= 0 {
		return c.handleError(err)
	}

	log := c.Log.WithField(LOG_FIELD_STATUS, log_status_retry)

	if c.options.retry.Always {
		log.Warnf(
			"%s -> has failed, will retry to run in %s: %s",
			c.GetFormattedCommand(),
			c.options.retry.Delay.String(),
			err,
		)
	} else {
		log.Warnf("%s -> has failed, will retry to run for %d more times in %s: %s", c.GetFormattedCommand(), c.options.retry.Tries, c.options.retry.Delay.String(), err)

		c.options.retry.Tries--
	}

	time.Sleep(c.options.retry.Delay)

	return c.pipe(runtime)
}

func (c *Command) resolveCommandRunner(runtime Runtime) CommandRunner {
	if runtime.CommandRunner != nil {
		return runtime.CommandRunner
	}

	if c.runtime.CommandRunner != nil {
		return c.runtime.CommandRunner
	}

	if c.T != nil && c.T.runtime.CommandRunner != nil {
		return c.T.runtime.CommandRunner
	}

	if c.TL != nil && c.TL.runtime.CommandRunner != nil {
		return c.TL.runtime.CommandRunner
	}

	if c.Plumber != nil && c.Plumber.runtime.CommandRunner != nil {
		return c.Plumber.runtime.CommandRunner
	}

	return NewCommandRunner()
}

func (c *Command) createInvocation() (CommandInvocation, error) {
	stdin, err := c.createStdin()
	if err != nil {
		return CommandInvocation{}, err
	}

	if c.credentialFn != nil {
		if c.Command.SysProcAttr == nil {
			c.Command.SysProcAttr = &syscall.SysProcAttr{}
		}

		if c.Command.SysProcAttr.Credential == nil {
			c.Command.SysProcAttr.Credential = &syscall.Credential{}
		}

		c.Command.SysProcAttr.Credential = c.credentialFn(c, c.Command.SysProcAttr.Credential)
	}

	name := ""
	args := []string{}
	if len(c.Command.Args) > 0 {
		name = c.Command.Args[0]
		args = append(args, c.Command.Args[1:]...)
	}

	return CommandInvocation{
		Name:          name,
		Args:          args,
		Formatted:     c.GetFormattedCommand(),
		Dir:           c.Command.Dir,
		Path:          c.Command.Path,
		Env:           append([]string{}, c.Command.Env...),
		Stdin:         stdin,
		ExtraFiles:    c.Command.ExtraFiles,
		SysProcAttr:   c.Command.SysProcAttr,
		EnsureIsAlive: c.options.ensureIsAlive,
		TaskName:      c.T.Name,
		TaskListName:  c.TL.Name,
		PlumberName:   c.Plumber.Cli.Name,
	}, nil
}

func (c *Command) createStdin() (io.Reader, error) {
	if c.scriptFn != nil {
		script := c.scriptFn(c)
		if script == nil {
			return c.Command.Stdin, nil
		}

		if script.File != "" {
			tpl, err := os.ReadFile(script.File)
			if err != nil {
				return nil, err
			}

			stdin, err := c.templateScript(script, string(tpl))
			if err != nil {
				return nil, err
			}

			c.Log.Tracef("Templated file for command script: %s -> with context %+v", script.File, script.Ctx)

			return stdin, nil
		}

		if script.Inline != "" {
			stdin, err := c.templateScript(script, script.Inline)
			if err != nil {
				return nil, err
			}

			c.Log.Tracef("Templated inline for command script: inline -> with context %+v", script.Ctx)

			return stdin, nil
		}

		return nil, fmt.Errorf("Either file or inline has to be set for command script.")
	}

	if c.stdinFn != nil {
		return c.stdinFn(c), nil
	}

	return c.Command.Stdin, nil
}

func (c *Command) resetStreams() {
	if c.options.recordStream {
		if c.lockStream == nil {
			c.lockStream = &sync.RWMutex{}
		}

		c.lockStream.Lock()
		c.combinedStream = []string{}
		c.stdoutStream = []string{}
		c.stderrStream = []string{}
		c.lockStream.Unlock()

		c.Log.Tracef("Resetting output streams: %s", c.GetFormattedCommand())
	}
}

func (c *Command) newStreamWriter(stream string, level LogLevel) io.Writer {
	return &commandStreamWriter{
		command: c,
		level:   level,
		stream:  stream,
	}
}

func (w *commandStreamWriter) Write(p []byte) (int, error) {
	w.buffer += string(p)

	for {
		index := strings.IndexByte(w.buffer, '\n')
		if index < 0 {
			break
		}

		line := w.buffer[:index+1]
		w.buffer = w.buffer[index+1:]
		w.command.handleStreamLine(w.stream, w.level, line)
	}

	return len(p), nil
}

func (c *Command) handleStreamLine(stream string, level LogLevel, line string) {
	log := c.Log.WithFields(logrus.Fields{})

	log.Logln(level, line)

	if c.options.recordStream {
		c.lockStream.Lock()
		c.combinedStream = append(c.combinedStream, line)

		switch stream {
		case stream_stdout:
			c.stdoutStream = append(c.stdoutStream, line)
		case stream_stderr:
			c.stderrStream = append(c.stderrStream, line)
		}
		c.lockStream.Unlock()
	}
}

// Handles the stop cases for command.
func (c *Command) handleStopCases() bool {
	if c.status.stopCases.handled {
		return c.status.stopCases.result
	}

	c.status.stopCases.handled = true

	if result := c.IsDisabled(); result {
		c.Log.WithField(LOG_FIELD_CONTEXT, log_context_disable).
			Debugf("%s", c.T.Name)

		c.status.stopCases.result = true
		return c.status.stopCases.result
	}

	c.status.stopCases.result = false
	return c.status.stopCases.result
}

// Handles the global plumber terminator to stop execution of the command and forwards the terminate signal if running.
func (c *Command) handleTerminator() {
	if c.IsDisabled() {
		c.Log.Tracef(
			"Deregister terminator directly because the command is already not available: %s",
			c.GetFormattedCommand(),
		)

		c.Plumber.DeregisterTerminator()

		return
	}

	ch := make(chan os.Signal, 1)
	c.Plumber.Terminator.ShouldTerminate.Register(ch)
	defer c.Plumber.Terminator.ShouldTerminate.Unregister(ch)

	sig := <-ch

	if c.Command.Process == nil {
		c.Log.Tracef("Already finished running, registered as terminated: %s", c.GetFormattedCommand())
		c.Plumber.RegisterTerminated()

		return
	}

	c.Log.Tracef("Forwarding signal to process: %s", sig)

	if err := c.Command.Process.Signal(sig); err != nil {
		c.Log.Tracef("Termination error: %s > %s", c.GetFormattedCommand(), err.Error())
	}

	if c.onTerminatorFn != nil {
		c.T.SendError(c.onTerminatorFn(c))
	}

	c.Log.Tracef("Registered as terminated: %s", c.GetFormattedCommand())

	c.Plumber.RegisterTerminated()
}

func (c *Command) templateScript(script *CommandScript, tmpl string) (io.Reader, error) {
	tpl, err := InlineTemplate(tmpl, script.Ctx, script.Funcs...)

	if err != nil {
		return nil, err
	}

	for _, t := range strings.Split(tpl, "\n") {
		c.Log.WithField(LOG_FIELD_STATUS, log_status_script).Infoln(t)
	}

	return strings.NewReader(tpl), nil
}
