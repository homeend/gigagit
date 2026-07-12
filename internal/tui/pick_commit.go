package tui

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// pickTarget identifies the commit entry `a` was pressed on in a switcher and
// carries everything the lanes need to dispatch without re-reading the popup
// (which may already be closed when the probe returns).
type pickTarget struct {
	sha      string // full commit sha (Bookmark.Commit / ShelfEntry.Origin.Commit)
	shelfID  string // non-empty = shelf entry (the patch lane is possible)
	hasPatch bool   // the shelf entry carries a stored patch blob
}

// pickProbeMsg is the async result of the commit-existence probe.
type pickProbeMsg struct {
	gen    int
	target pickTarget
	line   model.LogLine // short sha + subject when found
	found  bool
	err    error
}

// startPickCommit dispatches the gen-guarded existence probe for t. The lanes
// resolve on the probe's return (handlePickProbe); pickGen drops a result that
// arrives after the switcher was closed or the repo was switched.
func (m Model) startPickCommit(t pickTarget) (Model, tea.Cmd) {
	m.pickGen++
	gen := m.pickGen
	svc := m.svc
	return m, func() tea.Msg {
		line, found, err := svc.CommitLookup(context.Background(), t.sha)
		return pickProbeMsg{gen: gen, target: t, line: line, found: found, err: err}
	}
}

// handlePickProbe forks the three lanes: a live cherry-pick (commit exists), a
// stored-patch replay (shelf entry, commit gone), or a notice (nothing to do).
func (m Model) handlePickProbe(msg pickProbeMsg) (Model, tea.Cmd) {
	if msg.gen != m.pickGen || m.running {
		return m, nil // stale (switcher closed / repo switched) or an op raced in
	}
	if m.modal != nil {
		// Another dialog opened while the probe ran — never clobber it.
		m.statusMsg = "cherry-pick: cancelled (another dialog opened) — press a again"
		return m, nil
	}
	if msg.err != nil {
		m.statusMsg = "cherry-pick: " + msg.err.Error()
		return m, nil
	}
	t := msg.target
	branch := m.status.Branch
	if branch == "" {
		branch = "the current branch"
	}
	if msg.found {
		sha := t.sha
		m.modal = &decisionState{
			req: engine.DecisionRequest{
				ID:      "pick-commit",
				Prompt:  "Cherry-pick " + msg.line.Hash + " " + msg.line.Subject + " onto " + branch + "?",
				Options: []string{"Cherry-pick", "Cancel"},
			},
			onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
				if opt != "Cherry-pick" {
					return m, nil
				}
				// Close the switcher: a conflicted pick must land in the main
				// view, where the status refresh feeds the conflict process.
				m = m.clearLayers()
				return m.startOp(engine.CherryPick{Commit: sha})
			},
		}
		return m, nil
	}
	if t.shelfID != "" && t.hasPatch {
		short := t.sha
		if len(short) > 7 {
			short = short[:7]
		}
		id := t.shelfID
		m.modal = &decisionState{
			req: engine.DecisionRequest{
				ID:      "pick-commit-patch",
				Prompt:  "Commit " + short + " is no longer in the repo. Re-apply the shelved patch as a new commit?",
				Options: []string{"Apply patch", "Cancel"},
			},
			onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
				if opt != "Apply patch" {
					return m, nil
				}
				// Local blob read + temp write — fast, no git; the
				// bookmarkPastePrompt precedent for sync resolution in update.
				path, err := m.svc.ShelfPatchFile(context.Background(), id)
				if err != nil {
					m.statusMsg = "apply patch: " + err.Error()
					return m, nil
				}
				m.pickPatchTemp = path
				m = m.clearLayers()
				return m.startOp(engine.ApplyPatch{Path: path, Mode: engine.ApplyModeCommits})
			},
		}
		return m, nil
	}
	if t.shelfID != "" {
		m.statusMsg = "commit no longer exists and this entry has no stored patch (shelved before patch support, or a merge commit)"
	} else {
		m.statusMsg = "commit no longer exists — a bookmark stores no snapshot (shelve commits to keep them applyable)"
	}
	return m, nil
}

// cleanupPickPatchTemp removes the patch lane's temp file, if one is pending.
// Called when the op that consumed it finishes, and on reRoot.
func (m Model) cleanupPickPatchTemp() Model {
	if m.pickPatchTemp != "" {
		_ = os.Remove(m.pickPatchTemp)
		m.pickPatchTemp = ""
	}
	return m
}
