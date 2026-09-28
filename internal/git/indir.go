package git

import (
	"context"
	"path/filepath"

	"github.com/homeend/gigagit/internal/gitexec"
)

// InDir returns a view of the repository whose every git invocation runs
// against the worktree at dir (`git -C <dir> …`). dir must be a worktree top
// level of THIS repository; the view shares the runner (and so the
// subprocess cap and the span recorder) and pins Root so TopLevel answers
// without a process. Ops reach it through OpDeps.RepoAt.
func (r *Repo) InDir(dir string) *Repo {
	return &Repo{Runner: dirRunner{dir: dir, inner: r.Runner}, Root: filepath.Clean(dir)}
}

// dirRunner prefixes every argv with -C <dir>. It is the one place the
// prefix is spelled, so a FakeRunner test sees exactly "-C", dir, verb….
type dirRunner struct {
	dir   string
	inner gitexec.Runner
}

func (d dirRunner) prefix(argv []string) []string {
	out := make([]string, 0, len(argv)+2)
	out = append(out, "-C", d.dir)
	return append(out, argv...)
}

func (d dirRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	return d.inner.Run(ctx, name, d.prefix(argv))
}

func (d dirRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	return d.inner.RunEnv(ctx, name, d.prefix(argv), env)
}

func (d dirRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	return d.inner.Stream(ctx, name, d.prefix(argv), onLine)
}
