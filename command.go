package plumber

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/workanator/go-floc/v3"
	"gitlab.kilic.dev/libraries/go-utils/utils"
)

type Command[Pipe TaskListData] struct {
	Command     *exec.Cmd
	stdoutLevel logrus.Level
	stderrLevel logrus.Level
	stdout      output
	stderr      output
	task        *Task[Pipe]
	log         *logrus.Entry
}

type (
	output struct {
		closer io.ReadCloser
		reader *bufio.Reader
	}
	cmdFn[Pipe TaskListData] func(*Command[Pipe]) error
)

const (
	command_started  string = "RUN"
	command_failed   string = "FAIL"
	command_finished string = "END"
	command_exited   string = "EXIT"
)

// Command.New Creates a new command to be executed.
func (c *Command[Pipe]) New(task *Task[Pipe], command string, args ...string) *Command[Pipe] {
	c.Command = exec.Command(command, args...)
	c.task = task
	c.log = task.Log

	c.SetLogLevel(0, 0)

	return c
}

// Command.Set Sets the command details.
func (c *Command[Pipe]) Set(fn cmdFn[Pipe]) *Command[Pipe] {
	err := fn(c)

	if err != nil {
		c.task.Channel.Fatal <- err
	}

	return c
}

// Command.SetLogLevel Sets the log level specific to this command.
func (c *Command[Pipe]) SetLogLevel(stdout logrus.Level, stderr logrus.Level) *Command[Pipe] {
	if stdout == 0 {
		c.stdoutLevel = logrus.InfoLevel
	}

	if stderr == 0 {
		c.stderrLevel = logrus.WarnLevel
	}

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

// Command.Run Run the defined command.
func (c *Command[Pipe]) Run() error {
	cmd := strings.Join(c.Command.Args, " ")

	c.log.WithField("status", command_started).
		Infof("$ %s", cmd)

	c.Command.Args = utils.DeleteEmptyStringsFromSlice(c.Command.Args)

	if err := c.pipe(); err != nil {
		c.log.WithField("status", command_failed).
			Errorf("$ %s > %s", cmd, err.Error())

		return err
	}

	c.log.WithField("status", command_finished).Infof("$ %s", cmd)

	return nil
}

func (c *Command[Pipe]) Job() Job {
	return func(ctx floc.Context, ctrl floc.Control) error {
		return c.Run()
	}
}

func (c *Command[Pipe]) AddSelfToTheTask() *Command[Pipe] {
	c.task.AddCommands(c)

	return c
}

func (c *Command[Pipe]) AddSelfToParentTask(parent *Task[Pipe]) *Command[Pipe] {
	parent.AddCommands(c)

	return c
}

// Command.pipe Executes the command and pipes the output through the logger.
func (c *Command[Pipe]) pipe() error {
	cmd := strings.Join(c.Command.Args, " ")

	if err := c.createReaders(); err != nil {
		return err
	}

	if err := c.Command.Start(); err != nil {
		c.log.WithField("status", command_failed).
			Errorf("Can not start the command: $ %s", cmd)

		return err
	}

	go c.handleStream(c.stdout, c.stdoutLevel)
	go c.handleStream(c.stderr, c.stderrLevel)

	if err := c.Command.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				c.log.WithField("status", command_exited).
					Debugf("$ %s > Exit Code: %v", cmd, status.ExitStatus())
			}
		}

		return err
	}

	return nil
}

// Command.createReaders Creates closers and readers for stdout and stderr.
func (c *Command[Pipe]) createReaders() error {
	closer, err := c.Command.StdoutPipe()

	if err != nil {
		return fmt.Errorf("Failed creating command stdout pipe: %s", err)
	}

	reader := bufio.NewReader(closer)

	c.stdout = output{closer: closer, reader: reader}

	closer, err = c.Command.StderrPipe()

	if err != nil {
		return fmt.Errorf("Failed creating command stderr pipe: %s", err)
	}

	reader = bufio.NewReader(closer)

	c.stderr = output{closer: closer, reader: reader}

	return nil
}

// Command.handleStream Handles incoming data stream from a command.
func (c *Command[Pipe]) handleStream(output output, level logrus.Level) {
	defer output.closer.Close()

	log := c.log.WithFields(logrus.Fields{})

	for {
		str, err := output.reader.ReadString('\n')

		if err != nil {
			break
		}

		log.Logln(level, str)
	}
}
