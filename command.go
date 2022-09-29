package plumber

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"gitlab.kilic.dev/libraries/go-utils/utils"
)

type Command[Pipe TaskListData] struct {
	Command        *exec.Cmd
	stdoutLevel    LogLevel
	stderrLevel    LogLevel
	lifetimeLevel  LogLevel
	stdout         output
	stderr         output
	stdoutStream   []string
	stderrStream   []string
	combinedStream []string
	recordStream   bool
	ignoreError    bool
	task           *Task[Pipe]
	Log            *logrus.Entry
	setFn          CommandFn[Pipe]
	runAfterFn     CommandFn[Pipe]
	options        CommandOptions[Pipe]
	onTerminatorFn CommandFn[Pipe]
}

type CommandOptions[Pipe TaskListData] struct {
	Disable TaskPredicateFn[Pipe]
}

type (
	output struct {
		closer io.ReadCloser
		reader *bufio.Reader
		stream string
	}
	CommandFn[Pipe TaskListData] func(*Command[Pipe]) error
)

const (
	command_started  string = "RUN"
	command_failed   string = "FAIL"
	command_finished string = "END"
	command_exited   string = "EXIT"
	stream_stdout    string = "stdout"
	stream_stderr    string = "stderr"
)

func NewCommand[Pipe TaskListData](
	task *Task[Pipe],
	command string,
	args ...string,
) *Command[Pipe] {
	c := &Command[Pipe]{
		Command:        exec.Command(command, args...),
		task:           task,
		Log:            task.Log,
		stdoutStream:   []string{},
		stderrStream:   []string{},
		combinedStream: []string{},
	}

	c.options = CommandOptions[Pipe]{
		Disable: func(t *Task[Pipe]) bool {
			return false
		},
	}

	c.SetLogLevel(0, 0, 0)

	return c
}

// Command.Set Sets the command details.
func (c *Command[Pipe]) Set(fn CommandFn[Pipe]) *Command[Pipe] {
	c.setFn = fn

	return c
}

// Command.SetLogLevel Sets the log level specific to this command.
func (c *Command[Pipe]) SetLogLevel(
	stdout LogLevel,
	stderr LogLevel,
	lifetime LogLevel,
) *Command[Pipe] {
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

func (c *Command[Pipe]) SetIgnoreError() *Command[Pipe] {
	c.ignoreError = true

	return c
}

func (c *Command[Pipe]) ShouldDisable(fn TaskPredicateFn[Pipe]) *Command[Pipe] {
	c.options.Disable = fn

	return c
}

// Command.AppendArgs Appends arguments to the command.
func (c *Command[Pipe]) AppendArgs(args ...string) *Command[Pipe] {
	c.Command.Args = append(c.Command.Args, args...)

	return c
}

// Command.AppendEnvironment Appends environment variables to command as map.
func (c *Command[Pipe]) AppendEnvironment(environment map[string]string) *Command[Pipe] {
	for k, v := range environment {
		c.AppendDirectEnvironment(fmt.Sprintf("%s=%s", k, v))
	}

	return c
}

// Command.AppendDirectEnvironment Appends environment variables to command as directly.
func (c *Command[Pipe]) AppendDirectEnvironment(environment ...string) *Command[Pipe] {
	c.Command.Env = append(c.Command.Env, environment...)

	return c
}

// Command.SetDir Sets the directory of the command.
func (c *Command[Pipe]) SetDir(dir string) *Command[Pipe] {
	c.Command.Dir = dir

	return c
}

// Command.SetPath Sets the directory of the command.
func (c *Command[Pipe]) SetPath(dir string) *Command[Pipe] {
	c.Command.Path = dir

	return c
}

func (c *Command[Pipe]) EnableStreamRecording() *Command[Pipe] {
	c.recordStream = true

	return c
}

func (c *Command[Pipe]) RunSet() error {
	if c.setFn == nil {
		return nil
	}

	err := c.setFn(c)

	if err != nil {
		return err
	}

	return nil
}

// Command.Run Run the defined command.
func (c *Command[Pipe]) Run() error {
	if stop := c.handleStopCases(); stop {
		return nil
	}

	err := c.RunSet()

	if err != nil {
		return err
	}

	c.Log.WithField(LOG_FIELD_STATUS, command_started).
		Logf(c.lifetimeLevel, c.GetFormattedCommand())

	c.Command.Args = utils.DeleteEmptyStringsFromSlice(c.Command.Args)

	if err := c.pipe(); err != nil {
		c.Log.WithField(LOG_FIELD_STATUS, command_failed).
			Errorf("%s > %s", c.GetFormattedCommand(), err.Error())

		return err
	}

	if c.runAfterFn != nil {
		if err := c.runAfterFn(c); err != nil {
			return err
		}
	}

	c.Log.WithField(LOG_FIELD_STATUS, command_finished).Logf(c.lifetimeLevel, c.GetFormattedCommand())

	return nil
}

func (c *Command[Pipe]) Job() Job {
	return c.task.taskList.JobIfNot(
		c.task.taskList.Predicate(func(tl *TaskList[Pipe]) bool {
			return c.options.Disable(c.task)
		}),
		c.task.taskList.CreateJob(func(tl *TaskList[Pipe]) error {
			return c.Run()
		}),
		c.task.taskList.CreateJob(func(tl *TaskList[Pipe]) error {
			c.handleStopCases()

			return nil
		}),
	)
}

func (c *Command[Pipe]) AddSelfToTheTask() *Command[Pipe] {
	c.task.AddCommands(c)

	return c
}

func (c *Command[Pipe]) AddSelfToTheParentTask(pt *Task[Pipe]) *Command[Pipe] {
	pt.AddCommands(c)

	return c
}

func (c *Command[Pipe]) GetFormattedCommand() string {
	return fmt.Sprintf("$ %s", strings.Join(c.Command.Args, " "))
}

func (c *Command[Pipe]) EnableTerminator() *Command[Pipe] {
	go func() {
		signal := <-c.task.Plumber.Terminator.ShouldTerminate

		c.Log.Debugf("Forwarding signal to process: %s", signal)

		if err := c.Command.Process.Signal(signal); err != nil {
			c.Log.Tracef("Termination error: %s > %s", c.GetFormattedCommand(), err.Error())
		}

		if c.onTerminatorFn != nil {
			c.task.SendError(c.onTerminatorFn(c))
		}

		c.task.Plumber.SendTerminated()
	}()

	c.Log.Tracef("Registered terminator: %s", c.GetFormattedCommand())
	c.task.Plumber.RegisterTerminator()

	return c
}

func (c *Command[Pipe]) SetOnTerminator(fn CommandFn[Pipe]) *Command[Pipe] {
	c.onTerminatorFn = fn

	return c
}

func (c *Command[Pipe]) ShouldRunAfter(fn CommandFn[Pipe]) *Command[Pipe] {
	c.runAfterFn = fn

	return c
}

func (c *Command[Pipe]) GetStdoutStream() []string {
	if !c.recordStream {
		c.task.SendFatal(fmt.Errorf("Stream recording should be enabled to fetch the command output stream."))
	}

	return c.stdoutStream
}

func (c *Command[Pipe]) GetStderrStream() []string {
	if !c.recordStream {
		c.task.SendFatal(fmt.Errorf("Stream recording should be enabled to fetch the command output stream."))
	}

	return c.stderrStream
}

func (c *Command[Pipe]) GetCombinedStream() []string {
	if !c.recordStream {
		c.task.SendFatal(fmt.Errorf("Stream recording should be enabled to fetch the command output stream."))
	}

	return c.combinedStream
}

// Command.pipe Executes the command and pipes the output through the logger.
func (c *Command[Pipe]) pipe() error {
	if err := c.createReaders(); err != nil {
		return err
	}

	if err := c.Command.Start(); err != nil {
		c.Log.WithField(LOG_FIELD_STATUS, command_failed).
			Debugf("%s > Can not start command!", c.GetFormattedCommand())

		return err
	}

	go c.handleStream(c.stdout, c.stdoutLevel)
	go c.handleStream(c.stderr, c.stderrLevel)

	if err := c.Command.Wait(); err != nil {
		//nolint:errorlint
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				c.Log.WithField(LOG_FIELD_STATUS, command_exited).
					Debugf("%s > Exit Code: %v", c.GetFormattedCommand(), status.ExitStatus())
			}
		}

		if !c.ignoreError {
			return err
		}
	}

	return nil
}

// Command.createReaders Creates closers and readers for stdout and stderr.
func (c *Command[Pipe]) createReaders() error {
	closer, err := c.Command.StdoutPipe()

	if err != nil {
		return fmt.Errorf("Failed creating command stdout pipe: %w", err)
	}

	reader := bufio.NewReader(closer)

	c.stdout = output{closer: closer, reader: reader, stream: stream_stdout}

	closer, err = c.Command.StderrPipe()

	if err != nil {
		return fmt.Errorf("Failed creating command stderr pipe: %w", err)
	}

	reader = bufio.NewReader(closer)

	c.stderr = output{closer: closer, reader: reader, stream: stream_stderr}

	return nil
}

// Command.handleStream Handles incoming data stream from a command.
func (c *Command[Pipe]) handleStream(output output, level LogLevel) {
	defer output.closer.Close()

	log := c.Log.WithFields(logrus.Fields{})

	for {
		str, err := output.reader.ReadString('\n')

		if err != nil {
			break
		}

		log.Logln(level, str)

		if c.recordStream {
			c.combinedStream = append(c.combinedStream, str)

			switch output.stream {
			case stream_stdout:
				c.stdoutStream = append(c.stdoutStream, str)
			case stream_stderr:
				c.stderrStream = append(c.stderrStream, str)
			}
		}
	}
}

func (c *Command[Pipe]) handleStopCases() bool {
	if result := c.options.Disable(c.task); result {
		c.Log.WithField(LOG_FIELD_CONTEXT, task_disabled).
			Debugf("%s", c.task.Name)

		return true
	}

	return false
}
