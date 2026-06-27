package gitexec

import "context"

// gitConcurrency caps git subprocesses in flight across the process. 8 leaves
// the ~6-subprocess startup fan-out unthrottled while capping future fan-out
// (concurrent ops, group sync) so a slow 100GB repo is not hit by dozens of
// simultaneous git processes.
const gitConcurrency = 8

// gitSem is process-global so every ExecRunner shares one ceiling.
var gitSem = make(chan struct{}, gitConcurrency)

// LimitRunner wraps a Runner, bounding concurrent Run/Stream calls by the
// process-global git subprocess ceiling.
type LimitRunner struct{ inner Runner }

// NewLimitRunner returns inner wrapped with the concurrency bound.
func NewLimitRunner(inner Runner) Runner { return &LimitRunner{inner: inner} }

func (l *LimitRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	return l.RunEnv(ctx, name, argv, nil)
}

func (l *LimitRunner) RunEnv(ctx context.Context, name string, argv, env []string) (Result, error) {
	select {
	case gitSem <- struct{}{}:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	defer func() { <-gitSem }()
	return l.inner.RunEnv(ctx, name, argv, env)
}

func (l *LimitRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	select {
	case gitSem <- struct{}{}:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	defer func() { <-gitSem }()
	return l.inner.Stream(ctx, name, argv, onLine)
}
