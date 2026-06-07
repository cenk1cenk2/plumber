package plumber

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

type CommandRunner interface {
	Run(ctx context.Context, invocation CommandInvocation, runtime CommandRuntime) (CommandResult, error)
}

type CommandInvocation struct {
	Name          string
	Args          []string
	Formatted     string
	Dir           string
	Path          string
	Env           []string
	Stdin         io.Reader
	ExtraFiles    []*os.File
	SysProcAttr   *syscall.SysProcAttr
	EnsureIsAlive bool
	TaskName      string
	TaskListName  string
	PlumberName   string
}

type CommandRuntime struct {
	Stdout     io.Writer
	Stderr     io.Writer
	SetProcess func(*os.Process)
}

type CommandResult struct {
	Started      bool
	ExitCode     int
	Success      bool
	Exited       bool
	ProcessState *os.ProcessState
}

type commandRunner struct{}

type commandResultError struct {
	command  string
	exitCode int
}

func NewCommandRunner() CommandRunner {
	return &commandRunner{}
}

func (e *commandResultError) Error() string {
	return fmt.Sprintf("command exited with code %d: %s", e.exitCode, e.command)
}

func (r *commandRunner) Run(_ context.Context, invocation CommandInvocation, runtime CommandRuntime) (CommandResult, error) {
	if runtime.Stdout == nil {
		runtime.Stdout = io.Discard
	}

	if runtime.Stderr == nil {
		runtime.Stderr = io.Discard
	}

	command := exec.Command(invocation.Name, invocation.Args...) //nolint:gosec
	command.Dir = invocation.Dir
	command.Path = invocation.Path
	command.Env = invocation.Env
	command.ExtraFiles = invocation.ExtraFiles
	command.SysProcAttr = invocation.SysProcAttr
	command.Stdin = invocation.Stdin

	stdout, err := command.StdoutPipe()
	if err != nil {
		return CommandResult{}, fmt.Errorf("Failed creating command stdout pipe: %w", err)
	}

	stderr, err := command.StderrPipe()
	if err != nil {
		return CommandResult{}, fmt.Errorf("Failed creating command stderr pipe: %w", err)
	}

	if err := command.Start(); err != nil {
		return CommandResult{}, err
	}

	if runtime.SetProcess != nil {
		runtime.SetProcess(command.Process)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(runtime.Stdout, stdout)
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(runtime.Stderr, stderr)
	}()

	err = command.Wait()
	wg.Wait()

	result := CommandResult{
		Started:      true,
		ExitCode:     -1,
		ProcessState: command.ProcessState,
	}

	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
		result.Success = command.ProcessState.Success()
		result.Exited = command.ProcessState.Exited()
	}

	return result, err
}
