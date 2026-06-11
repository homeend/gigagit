// Package app wires the foundation layers into runnable commands. The `inspect`
// command is a temporary M1 surface; the TUI (Plan 3) will replace it.
package app

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/gigagit/gg/internal/buildinfo"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// Options configures an Inspect run.
type Options struct {
	WorkDir  string
	Stdout   io.Writer
	DumpPath string    // if non-empty, write a debug dump here
	Trace    io.Writer // if non-nil, enable verbose JSON-lines tracing
}

// Inspect opens the repo, prints a summary, and optionally writes a debug dump.
func Inspect(ctx context.Context, opts Options) error {
	ring := observ.NewRing(200)
	var rec observ.Recorder = ring
	if opts.Trace != nil {
		rec = observ.NewTraceRecorder(ring, opts.Trace)
	}
	runner := gitexec.NewExecRunner("git", opts.WorkDir, rec)
	repo := &git.Repo{Runner: runner}

	var errs []string
	st, err := repo.Status(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}
	branches, err := repo.Branches(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}
	wts, err := repo.Worktrees(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}

	c := st.Counts()
	fmt.Fprintf(opts.Stdout, "%s\n", buildinfo.String())
	fmt.Fprintf(opts.Stdout, "branch: %s", st.Branch)
	if st.Upstream != "" {
		fmt.Fprintf(opts.Stdout, " (upstream %s, ahead %d, behind %d)", st.Upstream, st.Ahead, st.Behind)
	}
	fmt.Fprintln(opts.Stdout)
	fmt.Fprintf(opts.Stdout, "changes: staged %d, unstaged %d, untracked %d, conflicted %d\n",
		c.Staged, c.Unstaged, c.Untracked, c.Conflicted)
	fmt.Fprintf(opts.Stdout, "branches: %d\n", len(branches))
	fmt.Fprintf(opts.Stdout, "worktrees: %d\n", len(wts))

	if opts.DumpPath != "" {
		if derr := DumpRepo(ctx, opts.DumpPath, repo, ring, errs); derr != nil {
			return fmt.Errorf("write debug dump: %w", derr)
		}
		fmt.Fprintf(opts.Stdout, "debug dump written: %s\n", opts.DumpPath)
	}
	return nil
}

// DumpRepo assembles and writes a debug dump for repo using ring's recent spans.
// It is best-effort: git failures degrade gracefully into the dump's fields.
func DumpRepo(ctx context.Context, path string, repo *git.Repo, ring *observ.Ring, errs []string) error {
	st, err := repo.Status(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}
	gitVer := ""
	if res, verr := repo.Runner.Run(ctx, "git version", gitcmd.New("version").ToArgv()); verr == nil {
		gitVer = strings.TrimSpace(res.Stdout)
	}
	c := st.Counts()
	d := observ.Dump{
		GeneratedAt: time.Now(),
		GGVersion:   buildinfo.Version,
		GitVersion:  gitVer,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Repo: observ.RepoInfo{
			Branch:   st.Branch,
			Upstream: st.Upstream,
		},
		WorkingTree: observ.DumpCounts{
			Staged: c.Staged, Unstaged: c.Unstaged,
			Untracked: c.Untracked, Conflicted: c.Conflicted,
		},
		Recent: ring.Snapshot(),
		Errors: errs,
	}
	return observ.WriteDump(path, d)
}
