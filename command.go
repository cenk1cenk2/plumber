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

type Command struct {
	cmd         *exec.Cmd
	stdoutLevel logrus.Level
	stderrLevel logrus.Level
	stdout      output
	stderr      output
	task        *Task[struct{}, struct{}]
	log         *logrus.Entry
}

type (
	output struct {
		closer io.ReadCloser
		reader *bufio.Reader
	}
)

const (
	command_started  string = "RUN"
	command_failed   string = "FAILED"
	command_finished string = "END"
	command_exited   string = "EXIT"
)

// Command.New Creates a new command to be executed.
func (c *Command) New(task *Task[struct{}, struct{}], command *exec.Cmd) *Command {
	c.cmd = command
	c.task = task
	c.log = task.Log

	c.SetLogLevel(0, 0)

	return c
}

// Command.SetLogLevel Sets the log level specific to this command.
func (c *Command) SetLogLevel(stdout logrus.Level, stderr logrus.Level) *Command {
	if stdout == 0 {
		c.stdoutLevel = logrus.InfoLevel
	}

	if stderr == 0 {
		c.stderrLevel = logrus.WarnLevel
	}

	return c
}

// Command.Run Run the defined command.
func (c *Command) Run() error {
	cmd := strings.Join(c.cmd.Args, " ")

	c.log.WithField("context", command_started).
		Infof("$ %s", cmd)

	c.cmd.Args = utils.DeleteEmptyStringsFromSlice(c.cmd.Args)

	if err := c.pipe(); err != nil {
		c.log.WithField("context", command_failed).
			Errorf("$ %s > %s", cmd, err.Error())

		return err
	}

	c.log.WithField("context", command_finished).Infof("$ %s", cmd)

	return nil
}

// Command.pipe Executes the command and pipes the output through the logger.
func (c *Command) pipe() error {
	cmd := strings.Join(c.cmd.Args, " ")

	if err := c.createReaders(); err != nil {
		return err
	}

	if err := c.cmd.Start(); err != nil {
		c.log.WithField("context", command_failed).
			Errorf("Can not start the command: $ %s", cmd)

		return err
	}

	go c.handleStream(c.stdout, c.stdoutLevel)
	go c.handleStream(c.stderr, c.stderrLevel)

	if err := c.cmd.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				c.log.WithField("context", command_exited).
					Debugf("$ %s > Exit Code: %v", cmd, status.ExitStatus())
			}
		}

		return err
	}

	return nil
}

// Command.createReaders Creates closers and readers for stdout and stderr.
func (c *Command) createReaders() error {
	closer, err := c.cmd.StdoutPipe()

	if err != nil {
		return fmt.Errorf("Failed creating command stdout pipe: %s", err)
	}

	reader := bufio.NewReader(closer)

	c.stdout = output{closer: closer, reader: reader}

	closer, err = c.cmd.StderrPipe()

	if err != nil {
		return fmt.Errorf("Failed creating command stderr pipe: %s", err)
	}

	reader = bufio.NewReader(closer)

	c.stderr = output{closer: closer, reader: reader}

	return nil
}

// Command.handleStream Handles incoming data stream from a command.
func (c *Command) handleStream(output output, level logrus.Level) {
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
