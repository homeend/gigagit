// Package gitexec runs the system git binary, records timing spans, and exposes
// a Runner interface with a fake for tests.
package gitexec

import (
	"context"
	"time"
)

// Result is the outcome of one git invocation.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Runner executes git commands. name is the human label recorded as a span;
// argv is the full git argument vector (from gitcmd.Builder.ToArgv()).
type Runner interface {
	Run(ctx context.Context, name string, argv []string) (Result, error)
	// RunEnv is Run with extra environment for this one invocation. Each env
	// entry is "KEY=VALUE" and is appended onto the inherited process
	// environment. A nil env behaves exactly like Run.
	RunEnv(ctx context.Context, name string, argv, env []string) (Result, error)
	Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error)
}
