package gitexec

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// FakeCall records one invocation of the fake runner.
type FakeCall struct {
	Name string
	Argv []string
	Env  []string
}

// FakeRunner is an in-memory Runner for tests. Run is safe for concurrent
// use (the domain Snapshot fan-out calls one runner from many goroutines).
type FakeRunner struct {
	mu        sync.Mutex
	responses map[string]Result
	errs      map[string]error
	handlers  map[string]func(ctx context.Context, argv []string) (Result, error)
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

// SetHandler registers a per-name callback that receives the call's ctx and may
// block (e.g. for ctx-cancellation tests). It takes precedence over SetResponse.
func (f *FakeRunner) SetHandler(name string, h func(ctx context.Context, argv []string) (Result, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.handlers == nil {
		f.handlers = map[string]func(context.Context, []string) (Result, error){}
	}
	f.handlers[name] = h
}

func (f *FakeRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	return f.RunEnv(ctx, name, argv, nil)
}

func (f *FakeRunner) RunEnv(ctx context.Context, name string, argv, env []string) (Result, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{Name: name, Argv: argv, Env: env})
	h := f.handlers[name]
	err := f.errs[name]
	r, ok := f.responses[name]
	f.mu.Unlock()
	if h != nil {
		return h(ctx, argv)
	}
	if err != nil {
		return r, err
	}
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
