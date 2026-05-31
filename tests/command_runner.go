package tests

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/cenk1cenk2/plumber/v6"
	"github.com/cenk1cenk2/plumber/v6/tests/mocks"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
)

type TestingCommandRunner struct {
	Mock *mocks.MockCommandRunner

	lock        sync.Mutex
	responses   []TestingCommandResponse
	invocations []plumber.CommandInvocation
}

type TestingCommandResponse struct {
	Name   string
	Args   []string
	Match  func(plumber.CommandInvocation) bool
	Stdout string
	Stderr string
	Result *plumber.CommandResult
	Err    error
}

func NewTestingCommandRunner() *TestingCommandRunner {
	GinkgoHelper()

	runner := &TestingCommandRunner{
		Mock: mocks.NewMockCommandRunner(GinkgoT()),
	}

	runner.Mock.EXPECT().
		Run(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(runner.run).
		Maybe()

	return runner
}

func NewMockCommandRunner() *mocks.MockCommandRunner {
	GinkgoHelper()

	return mocks.NewMockCommandRunner(GinkgoT())
}

func TestingCommandSuccess() plumber.CommandResult {
	return plumber.CommandResult{
		Started:  true,
		ExitCode: 0,
		Success:  true,
		Exited:   true,
	}
}

func TestingCommandFailure(exitCode int) plumber.CommandResult {
	return plumber.CommandResult{
		Started:  true,
		ExitCode: exitCode,
		Success:  false,
		Exited:   true,
	}
}

func (r *TestingCommandRunner) Runner() plumber.CommandRunner {
	GinkgoHelper()

	return r.Mock
}

func (r *TestingCommandRunner) Add(response TestingCommandResponse) *TestingCommandRunner {
	GinkgoHelper()

	r.lock.Lock()
	r.responses = append(r.responses, response)
	r.lock.Unlock()

	return r
}

func (r *TestingCommandRunner) AddResponses(responses ...TestingCommandResponse) *TestingCommandRunner {
	GinkgoHelper()

	r.lock.Lock()
	r.responses = append(r.responses, responses...)
	r.lock.Unlock()

	return r
}

func (r *TestingCommandRunner) Invocations() []plumber.CommandInvocation {
	GinkgoHelper()

	r.lock.Lock()
	defer r.lock.Unlock()

	invocations := make([]plumber.CommandInvocation, len(r.invocations))
	copy(invocations, r.invocations)

	return invocations
}

func (r *TestingCommandRunner) InvocationNames() []string {
	GinkgoHelper()

	invocations := r.Invocations()
	names := make([]string, len(invocations))
	for i, invocation := range invocations {
		names[i] = invocation.Name
	}

	return names
}

func (r *TestingCommandRunner) LastInvocation() (plumber.CommandInvocation, bool) {
	GinkgoHelper()

	invocations := r.Invocations()
	if len(invocations) == 0 {
		return plumber.CommandInvocation{}, false
	}

	return invocations[len(invocations)-1], true
}

func ReadInvocationStdin(invocation plumber.CommandInvocation) (string, error) {
	GinkgoHelper()

	if invocation.Stdin == nil {
		return "", nil
	}

	data, err := io.ReadAll(invocation.Stdin)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (r *TestingCommandRunner) run(
	_ context.Context,
	invocation plumber.CommandInvocation,
	runtime plumber.CommandRuntime,
) (plumber.CommandResult, error) {
	response, err := r.popResponse(invocation)
	if err != nil {
		return plumber.CommandResult{}, err
	}

	if response.Stdout != "" && runtime.Stdout != nil {
		if _, err := runtime.Stdout.Write([]byte(response.Stdout)); err != nil {
			return plumber.CommandResult{}, err
		}
	}

	if response.Stderr != "" && runtime.Stderr != nil {
		if _, err := runtime.Stderr.Write([]byte(response.Stderr)); err != nil {
			return plumber.CommandResult{}, err
		}
	}

	result := TestingCommandSuccess()
	if response.Result != nil {
		result = *response.Result
	}

	return result, response.Err
}

func (r *TestingCommandRunner) popResponse(invocation plumber.CommandInvocation) (TestingCommandResponse, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.invocations = append(r.invocations, invocation)

	if len(r.responses) == 0 {
		return TestingCommandResponse{}, nil
	}

	for i, response := range r.responses {
		if !response.matches(invocation) {
			continue
		}

		r.responses = append(r.responses[:i], r.responses[i+1:]...)

		return response, nil
	}

	return TestingCommandResponse{}, fmt.Errorf("no testing command response matched: %s", invocation.Formatted)
}

func (r TestingCommandResponse) matches(invocation plumber.CommandInvocation) bool {
	if r.Match != nil && !r.Match(invocation) {
		return false
	}

	if r.Name != "" && r.Name != invocation.Name {
		return false
	}

	if r.Args != nil && !reflect.DeepEqual(r.Args, invocation.Args) {
		return false
	}

	return true
}
