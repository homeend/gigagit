package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// ConflictState attributes the current conflict — or a paused sequencer op —
// to the operation that produced it. Op != "" whenever a merge/rebase/
// cherry-pick/revert is in progress: with unresolved conflicts, OR paused
// with everything resolved (e.g. resolved outside gg but never continued).
// The zero value (Op == "") means no such op is in progress (e.g. a
// stash-pop conflict — there is no source to show). Callers distinguish
// "conflicted" from "paused, resolved" via the status's own conflicted-file
// count.
type ConflictState struct {
	Op     string // "merge" | "rebase" | "cherry-pick" | "revert" | ""
	Source string // branch being merged / rebased, or the picked/reverted commit
	Target string // branch merged-into / rebased-onto
}

// Describe renders the human phrase, or "" when there is nothing to attribute.
// Translated sibling: tui's describeConflict (+ its parity test) — keep in lockstep.
func (c ConflictState) Describe() string {
	switch {
	case c.Op == "merge" && c.Source != "" && c.Target != "":
		return "merging " + c.Source + " into " + c.Target
	case c.Op == "rebase" && c.Source != "" && c.Target != "":
		return "rebasing " + c.Source + " onto " + c.Target
	case c.Op == "cherry-pick" && c.Source != "":
		return "cherry-picking " + c.Source
	case c.Op == "revert" && c.Source != "":
		return "reverting " + c.Source
	}
	return ""
}

// Conflict derives the conflict/paused-op state from a status the caller
// already read. It is the public face of conflictState, used by the TUI's
// status source so a per-panel status refresh carries the same attribution
// the full Snapshot did. Cheap on the steady-state clean path: once the git
// dir is cached, a clean working tree with no paused op returns after pure
// file stats — zero git invocations and no gate. Otherwise the git probes
// run under their own Read reservation. The reservation is taken HERE, not
// inside conflictState — the helper's other caller (loadSnapshot) already
// holds one, and a nested Read can deadlock behind a queued writer under the
// gate's writer-preferring FIFO.
func (s *Service) Conflict(ctx context.Context, st model.WorkingTreeStatus) ConflictState {
	if st.Counts().Conflicted == 0 {
		if d := s.cachedGitDir(); d != "" && git.PausedOpIn(d) == "" {
			return ConflictState{}
		}
		// First call (git dir not yet cached) or a paused op detected:
		// fall through to the gated read.
	}
	cs, err := query(ctx, s, "conflict:"+st.Branch, func(ctx context.Context) (ConflictState, error) {
		return s.conflictState(ctx, st), nil
	})
	if err != nil {
		// Gate acquisition failed (ctx cancelled mid-refresh): no attribution
		// this round; the next status refresh retries.
		return ConflictState{}
	}
	return cs
}

// conflictState attributes st's conflicts — or a paused sequencer op whose
// conflicts were all resolved (e.g. outside gg) — to the operation in
// progress. With unmerged files present it probes via the git verbs exactly
// as before; with none it falls back to the stat-level PausedOpIn probe, so
// a resolved-but-not-continued rebase is still reported. During a rebase
// HEAD is detached, so the rebase target comes from the rebase state
// (RebaseParties), not st.Branch. It assumes the caller holds a Read
// reservation (Conflict and loadSnapshot both do) — it must not acquire its
// own.
func (s *Service) conflictState(ctx context.Context, st model.WorkingTreeStatus) ConflictState {
	if st.Counts().Conflicted > 0 {
		if ok, err := s.repo.MergeInProgress(ctx, ""); err == nil && ok {
			return s.attributeOp(ctx, "merge", st)
		}
		if ok, err := s.repo.RebaseInProgress(ctx, ""); err == nil && ok {
			return s.attributeOp(ctx, "rebase", st)
		}
		// Cherry-pick/revert probed last: a paused rebase pick also sets
		// CHERRY_PICK_HEAD.
		if ok, err := s.repo.CherryPickInProgress(ctx, ""); err == nil && ok {
			return s.attributeOp(ctx, "cherry-pick", st)
		}
		if ok, err := s.repo.RevertInProgress(ctx, ""); err == nil && ok {
			return s.attributeOp(ctx, "revert", st)
		}
		return ConflictState{}
	}
	d := s.gitDirCached(ctx)
	if d == "" {
		return ConflictState{}
	}
	if op := git.PausedOpIn(d); op != "" {
		return s.attributeOp(ctx, op, st)
	}
	return ConflictState{}
}

// attributeOp fills Source/Target for a detected op. Best-effort: a failed
// read leaves the field empty rather than erroring — the Op alone is enough
// to drive the UI.
func (s *Service) attributeOp(ctx context.Context, op string, st model.WorkingTreeStatus) ConflictState {
	switch op {
	case "merge":
		src, _ := s.repo.MergeHeadName(ctx, "")
		return ConflictState{Op: "merge", Source: src, Target: st.Branch}
	case "rebase":
		branch, onto, _ := s.repo.RebaseParties(ctx, "")
		return ConflictState{Op: "rebase", Source: branch, Target: onto}
	case "cherry-pick":
		src, _ := s.repo.CherryPickHeadSummary(ctx, "")
		return ConflictState{Op: "cherry-pick", Source: src}
	case "revert":
		src, _ := s.repo.RevertHeadSummary(ctx, "")
		return ConflictState{Op: "revert", Source: src}
	}
	return ConflictState{}
}

// cachedGitDir returns the memoized git dir, or "" when not yet resolved.
// Lock-only — safe to call outside any gate reservation.
func (s *Service) cachedGitDir() string {
	s.gitDirMu.Lock()
	defer s.gitDirMu.Unlock()
	return s.gitDirPath
}

// gitDirCached resolves and memoizes this worktree's git dir. The first call
// runs one git invocation, so the caller must hold a Read reservation (its
// only caller, conflictState, always does). A failed resolution returns ""
// and retries on the next call.
func (s *Service) gitDirCached(ctx context.Context) string {
	s.gitDirMu.Lock()
	defer s.gitDirMu.Unlock()
	if s.gitDirPath == "" {
		if d, err := s.repo.GitDir(ctx); err == nil {
			s.gitDirPath = d
		}
	}
	return s.gitDirPath
}

// InProgressOp reports "merge", "rebase", "cherry-pick", "revert", or "" for the
// current working tree. Cherry-pick/revert are probed LAST (a paused rebase pick
// also sets CHERRY_PICK_HEAD, so rebase must win).
func (s *Service) InProgressOp(ctx context.Context) (string, error) {
	return query(ctx, s, "inprogress", func(ctx context.Context) (string, error) {
		if ok, err := s.repo.MergeInProgress(ctx, ""); err == nil && ok {
			return "merge", nil
		}
		if ok, err := s.repo.RebaseInProgress(ctx, ""); err == nil && ok {
			return "rebase", nil
		}
		if ok, err := s.repo.CherryPickInProgress(ctx, ""); err == nil && ok {
			return "cherry-pick", nil
		}
		if ok, err := s.repo.RevertInProgress(ctx, ""); err == nil && ok {
			return "revert", nil
		}
		return "", nil
	})
}

// ConflictPickerFile returns the marker text the hunk picker should parse for
// an unmerged path, plus the marker size to parse it with. The text is
// regenerated from the index stages via merge-file, with markers sized past
// the longest marker-like run in any input — so content that itself looks
// like conflict markers (a conflict once committed unresolved, a =======
// underline) stays plain content instead of derailing the parse. When the
// stages cannot be read (path not unmerged, or a side missing) it falls back
// to the working-tree bytes at git's default size 7 — exactly the previous
// behavior.
func (s *Service) ConflictPickerFile(ctx context.Context, path string) ([]byte, int, error) {
	type sized struct {
		content []byte
		size    int
	}
	res, err := query(ctx, s, "conflict-picker-file:"+path, func(c context.Context) (sized, error) {
		base, cur, inc, err := s.repo.UnmergedStages(c, path)
		if err != nil {
			return sized{}, err
		}
		size := conflictMarkerSize(base, cur, inc)
		out, err := s.repo.RegenerateConflict(c, base, cur, inc, size)
		if err != nil {
			return sized{}, err
		}
		return sized{content: out, size: size}, nil
	})
	if err != nil {
		content, ferr := s.WorktreeFile(ctx, path)
		return content, 7, ferr
	}
	return res.content, res.size, nil
}

// conflictMarkerSize picks a marker length no content line can imitate: eight
// past the longest line-leading run of '<', '=', or '>' in any input (git's
// default 7 as the floor).
func conflictMarkerSize(inputs ...[]byte) int {
	longest := 0
	for _, in := range inputs {
		for _, ln := range strings.Split(string(in), "\n") {
			if ln == "" {
				continue
			}
			c := ln[0]
			if c != '<' && c != '=' && c != '>' {
				continue
			}
			n := 0
			for n < len(ln) && ln[n] == c {
				n++
			}
			if n > longest {
				longest = n
			}
		}
	}
	if longest+8 < 7 {
		return 7
	}
	return longest + 8
}
