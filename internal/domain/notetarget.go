package domain

// The shared "which diff does this note belong to" rule (spec §4.5). The CLI
// and MCP both call it, so `gg note add`, `gg note apply`, `gg review --notes`
// and the MCP tools can never disagree about what a target flag means:
//
//	Target flags   | Address.State                       | old side | new side
//	---------------|-------------------------------------|----------|----------
//	(none)         | StateUnstaged, or StateUntracked    | index    | worktree
//	               | when git status lists it untracked  |          |
//	--cached       | StateStaged                         | HEAD     | index
//	--rev <commit> | StateCommitted, Commit = FULL sha   | parent   | commit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNoteTargetUsage marks a caller MISTAKE in the target flags (a range where
// a commit is required, --cached together with --rev, a missing or escaping
// path). Frontends map it to a usage exit code (the CLI: 2); every other error
// from NoteTarget is a genuine failure (exit 1).
var ErrNoteTargetUsage = errors.New("note target usage")

// NoteTarget resolves a path plus the --cached/--rev flags into the address a
// note hangs off. rev is resolved to a full sha here so the stored target is
// stable; a rev naming a RANGE is refused, because a note anchors to exactly
// one pair of texts.
func (s *Service) NoteTarget(ctx context.Context, path string, cached bool, rev string) (model.FileAddress, error) {
	if cached && rev != "" {
		return model.FileAddress{}, fmt.Errorf("%w: --cached and --rev are mutually exclusive", ErrNoteTargetUsage)
	}
	if strings.TrimSpace(path) == "" {
		return model.FileAddress{}, fmt.Errorf("%w: a note needs a file path", ErrNoteTargetUsage)
	}
	p := toGitPath(path)
	if p == ".." || strings.HasPrefix(p, "../") || filepath.IsAbs(path) {
		return model.FileAddress{}, fmt.Errorf("%w: path escapes the repository: %s", ErrNoteTargetUsage, path)
	}
	if rev != "" {
		if strings.Contains(rev, "..") {
			return model.FileAddress{}, fmt.Errorf("%w: a note anchors to one commit; pass the tip commit", ErrNoteTargetUsage)
		}
		full, err := s.RevParse(ctx, rev)
		if err != nil {
			return model.FileAddress{}, fmt.Errorf("unknown revision %q: %w", rev, err)
		}
		return model.FileAddress{State: model.StateCommitted, Commit: strings.TrimSpace(full), Path: p}, nil
	}
	if cached {
		return model.FileAddress{State: model.StateStaged, Path: p}, nil
	}
	// A file git has never seen has no index side, so its note must be stamped
	// StateUntracked or the sweep reads the wrong (absent) old side and drops it.
	st, err := s.Status(ctx)
	if err != nil {
		return model.FileAddress{}, err
	}
	for _, f := range st.Files {
		if toGitPath(f.Path) == p && f.Kind == model.KindUntracked {
			return model.FileAddress{State: model.StateUntracked, Path: p}, nil
		}
	}
	return model.FileAddress{State: model.StateUnstaged, Path: p}, nil
}
