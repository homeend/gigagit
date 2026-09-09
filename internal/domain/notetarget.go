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
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// NoteAuthorDefault picks the author label a note is stamped with when the
// caller gave none: an explicit given value wins, else $GG_AGENT (the agent
// harness's own name), else the literal "agent". The CLI (noteAuthorDefault)
// and the MCP note tools (gg_note_add, gg_notes_apply) both call this so an
// agent driving gg through either door gets the same default identity.
func NoteAuthorDefault(given string) string {
	if a := strings.TrimSpace(given); a != "" {
		return a
	}
	if a := strings.TrimSpace(os.Getenv("GG_AGENT")); a != "" {
		return a
	}
	return "agent"
}

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
		// ResolveRev peels to ^{commit}: an annotated tag resolves to the
		// commit it points at (never its own tag object id), and a non-commit
		// object (a blob, a tree) is refused the same as an unknown rev — a
		// note anchors to a commit, never to arbitrary git content.
		full, found, err := s.ResolveRev(ctx, rev)
		if err != nil {
			return model.FileAddress{}, fmt.Errorf("unknown revision %q: %w", rev, err)
		}
		if !found {
			return model.FileAddress{}, fmt.Errorf("unknown revision %q", rev)
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
