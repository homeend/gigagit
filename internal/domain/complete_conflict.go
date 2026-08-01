package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
)

// CompleteConflictResult is what a resolve-and-complete agent run produced.
type CompleteConflictResult struct {
	Overview    string // the agent's overview (may be "" — not an error)
	Op          string // the op that was paused when the run started
	StillPaused bool   // true when the agent stopped early (op still paused)
}

// CompleteConflictReport runs a conflict_complete agent command headless
// against the currently paused operation and reports its overview. The
// command arrives as TEMPLATE text (the config block); the engine op resolves
// it after creating the context file. Refuses when nothing is paused.
// Display-only: unlike ReviewReport nothing is persisted, and an empty
// overview is not an error (TUI parity).
func (s *Service) CompleteConflictReport(ctx context.Context, commandTemplate string, env []string) (CompleteConflictResult, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return CompleteConflictResult{}, err
	}
	cs := s.Conflict(ctx, st)
	if cs.Op == "" {
		return CompleteConflictResult{}, fmt.Errorf("no paused operation to complete")
	}
	files := unmergedPaths(st)
	top, err := s.TopLevel(ctx)
	if err != nil {
		return CompleteConflictResult{}, err
	}
	res, err := s.Execute(ctx, engine.CompleteConflict{
		Command: commandTemplate, Dir: top, Env: env,
		Op: cs.Op, Source: cs.Source, Target: cs.Target, ConflictedFiles: files,
	}, nil, nil)
	if err != nil {
		return CompleteConflictResult{}, err
	}
	overview := strings.TrimSpace(res.Captured)
	if overview != "" {
		// Unwrap a JSON-enveloped stdout (Claude --output-format json) the
		// way ReviewReport does; plain text passes through unchanged.
		report, perr := exttool.ParseCaptureReport(res.Captured)
		if perr != nil {
			return CompleteConflictResult{}, perr
		}
		overview = strings.TrimSpace(report)
	}
	out := CompleteConflictResult{Overview: overview, Op: cs.Op}
	if st2, serr := s.Status(ctx); serr == nil {
		out.StillPaused = s.Conflict(ctx, st2).Op != ""
	}
	return out, nil
}

// unmergedPaths lists the conflicted paths in status order, reusing
// model.WorkingTreeStatus.Conflicts() (model/conflict.go) rather than
// re-walking st.Files.
func unmergedPaths(st model.WorkingTreeStatus) []string {
	cs := st.Conflicts()
	out := make([]string, len(cs))
	for i, f := range cs {
		out[i] = f.Path
	}
	return out
}
