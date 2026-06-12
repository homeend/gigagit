// Package cli implements gigagit's scriptable command-line frontend over the
// shared engine.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// openRepo builds a Repo rooted at workdir with an always-on span ring.
func openRepo(workdir string) *git.Repo {
	return &git.Repo{Runner: gitexec.NewExecRunner("git", workdir, observ.NewRing(200))}
}

// cliDecider resolves engine forks from a flag-supplied policy, falling back to
// an interactive stdin prompt, and erroring when neither can answer.
type cliDecider struct {
	policy      map[string]string
	in          io.Reader
	out         io.Writer
	interactive bool
}

func (d cliDecider) Decide(_ context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	if opt, ok := d.policy[req.ID]; ok {
		return engine.DecisionResponse{Option: opt}, nil
	}
	if !d.interactive || d.in == nil {
		return engine.DecisionResponse{}, fmt.Errorf(
			"%s needs a decision (options: %s); rerun with the matching flag",
			req.ID, strings.Join(req.Options, ", "))
	}
	fmt.Fprintf(d.out, "%s\n  options: %s\n> ", req.Prompt, strings.Join(req.Options, ", "))
	line, _ := bufio.NewReader(d.in).ReadString('\n')
	choice := strings.TrimSpace(line)
	for _, o := range req.Options {
		if o == choice {
			return engine.DecisionResponse{Option: o}, nil
		}
	}
	return engine.DecisionResponse{}, fmt.Errorf("invalid choice %q for %s", choice, req.ID)
}

// syncWriter serializes writes to one underlying writer. The decider may
// prompt from the operation goroutine while runOperation prints progress from
// the main goroutine; both share stderr, so the writer must be safe for
// concurrent use (a bytes.Buffer in tests is not).
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// runOperation runs op, printing each Progress step to progress, and returns the
// operation result. The op runs in a goroutine so events stream live; decisions
// are resolved by dec (which may prompt). The whole run is reported to the span
// sink as one "op <Type>" span.
func runOperation(ctx context.Context, repo *git.Repo, op engine.Operation, dec engine.Decider, progress io.Writer) (engine.Result, error) {
	opStart := time.Now()
	events := make(chan engine.Event, 32)
	var (
		res engine.Result
		err error
	)
	done := make(chan struct{})
	go func() {
		res, err = op.Run(ctx, engine.OpDeps{Repo: repo, Events: events, Decider: dec})
		close(events)
		close(done)
	}()
	for e := range events {
		if p, ok := e.(engine.Progress); ok {
			if p.Detail != "" {
				fmt.Fprintf(progress, "→ %s: %s\n", p.Step, p.Detail)
			} else {
				fmt.Fprintf(progress, "→ %s\n", p.Step)
			}
		}
	}
	<-done
	span := observ.Span{Name: "op " + engine.OpName(op), Start: opStart, Duration: time.Since(opStart)}
	if err != nil {
		span.ExitCode = 1
		span.Err = err.Error()
	}
	observ.EmitSpan(span)
	return res, err
}
