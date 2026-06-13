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

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

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

// runOperation runs op through the domain layer (which serializes it on the
// repo's gate and emits the op span), printing each Progress step to
// progress. The op runs in a goroutine so events stream live; decisions are
// resolved by dec (which may prompt).
func runOperation(ctx context.Context, svc *domain.Service, op engine.Operation, dec engine.Decider, progress io.Writer) (engine.Result, error) {
	events := make(chan engine.Event, 32)
	var (
		res engine.Result
		err error
	)
	done := make(chan struct{})
	go func() {
		res, err = svc.Execute(ctx, op, events, dec)
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
	return res, err
}
