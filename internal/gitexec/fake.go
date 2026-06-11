package gitexec

import (
	"context"
	"fmt"
	"strings"
)

// FakeCall records one invocation of the fake runner.
type FakeCall struct {
	Name string
	Argv []string
}

// FakeRunner is an in-memory Runner for tests.
type FakeRunner struct {
	responses map[string]Result
	errs      map[string]error
	Calls     []FakeCall
}

// NewFakeRunner returns an empty fake runner.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{responses: map[string]Result{}, errs: map[string]error{}}
}

// SetResponse configures the Result returned for a given span name.
func (f *FakeRunner) SetResponse(name string, r Result) { f.responses[name] = r }

// SetError configures an error returned for a given span name.
func (f *FakeRunner) SetError(name string, err error) { f.errs[name] = err }

func (f *FakeRunner) Run(_ context.Context, name string, argv []string) (Result, error) {
	f.Calls = append(f.Calls, FakeCall{Name: name, Argv: argv})
	if err := f.errs[name]; err != nil {
		return f.responses[name], err
	}
	r, ok := f.responses[name]
	if !ok {
		return Result{}, fmt.Errorf("fake: no response configured for %q", name)
	}
	return r, nil
}

func (f *FakeRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	r, err := f.Run(ctx, name, argv)
	if err != nil {
		return r, err
	}
	for _, line := range strings.Split(strings.TrimRight(r.Stdout, "\n"), "\n") {
		if line != "" || r.Stdout != "" {
			onLine(line)
		}
	}
	return r, nil
}
