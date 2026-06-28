package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// remoteTagsMsg carries the result of a remote-tag lookup. manual=true means a
// user-initiated refresh (errors go to the status line); false means a silent
// background poll (errors discarded — see queryQuiet's no-record contract).
// dur is the wall-clock time of the lookup, used by the background lane for
// rolling duration stats (informational only, never affects scheduling).
// gen is the loadGen value captured at launch; the handler drops the message
// if it no longer matches m.loadGen (repo was switched while the read was
// in flight), mirroring the bgFetchDoneMsg pattern.
type remoteTagsMsg struct {
	names  map[string]bool
	err    error
	dur    time.Duration
	manual bool
	gen    int
}

// remoteTagsCmd runs the (network) remote-tag lookup off the UI thread. Shared by
// the manual .-menu action and the background scheduler lane.
func (m Model) remoteTagsCmd(ctx context.Context, manual bool) tea.Cmd {
	svc := m.svc
	gen := m.loadGen // snapshot at launch; handler drops stale results on repo switch
	return func() tea.Msg {
		start := time.Now()
		names, err := svc.RemoteTags(ctx)
		return remoteTagsMsg{names: names, err: err, dur: time.Since(start), manual: manual, gen: gen}
	}
}

// autoRemoteTagsEnabled reports whether a tag-window change should auto-trigger
// a background remote-tag lookup (default on; inverted config flag).
func (m Model) autoRemoteTagsEnabled() bool {
	return !m.cfg.Refresh.DisableRemoteTagsAuto
}

// applyPendingRemoteTag folds a pending optimistic add/remove into the set on op
// success, then clears the pending fields. Lazy-inits the map for an add.
func (m Model) applyPendingRemoteTag() Model {
	if m.pendingRemoteTagSet != "" {
		if m.remoteTagNames == nil {
			m.remoteTagNames = map[string]bool{}
		}
		m.remoteTagNames[m.pendingRemoteTagSet] = true
		m.pendingRemoteTagSet = ""
	}
	if m.pendingRemoteTagUnset != "" {
		delete(m.remoteTagNames, m.pendingRemoteTagUnset)
		m.pendingRemoteTagUnset = ""
	}
	// Drain the multi-tag add slice (set by the PushTags chain after a branch push).
	for _, n := range m.pendingRemoteTagAdds {
		if m.remoteTagNames == nil {
			m.remoteTagNames = map[string]bool{}
		}
		m.remoteTagNames[n] = true
	}
	m.pendingRemoteTagAdds = nil
	return m
}

// tagsAtCommit returns the local tags whose target commit is hash.
// Both model.Tag.Target and model.Branch.Hash are objectname:short from the
// same repo, so == comparison is correct.
func tagsAtCommit(tags []model.Tag, hash string) []model.Tag {
	if hash == "" {
		return nil
	}
	var out []model.Tag
	for _, t := range tags {
		if t.Target == hash {
			out = append(out, t)
		}
	}
	return out
}

// currentBranchTipHash returns the short commit hash at the current branch tip.
func (m Model) currentBranchTipHash() string {
	for _, b := range m.branches {
		if b.Name == m.status.Branch {
			return b.Hash
		}
	}
	return ""
}

// pushCurrentOp builds the Push operation for the current branch.
func (m Model) pushCurrentOp() engine.Operation {
	return engine.Push{Remote: "origin", Branch: m.status.Branch, SetUpstream: true}
}

// pushTagCheckMsg carries the result of a 5s-budgeted pre-push remote-tag check.
// remoteSet is nil on timeout or error — the handler treats nil as "skip the tag
// check and push directly" so P never hangs.
type pushTagCheckMsg struct {
	gen       int
	tipTags   []model.Tag
	remoteSet map[string]bool // nil on timeout/error → skip the tag check
	err       error
}

// pushTagCheckCmd runs a fresh, 5s-bounded remote-tag lookup off the UI thread.
// gen must equal m.pushCheckGen at launch so the handler can drop stale results.
func (m Model) pushTagCheckCmd(gen int, tipTags []model.Tag) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		set, err := svc.RemoteTagsFresh(ctx)
		return pushTagCheckMsg{gen: gen, tipTags: tipTags, remoteSet: set, err: err}
	}
}

// startPush begins a current-branch push. If the branch tip has local tags, it
// first runs a 5s-budgeted remote-tag check to offer pushing unpushed tip tags;
// otherwise it pushes the branch directly (no network call).
func (m Model) startPush() (tea.Model, tea.Cmd) {
	tipTags := tagsAtCommit(m.tags, m.currentBranchTipHash())
	if len(tipTags) == 0 {
		return m.startOp(m.pushCurrentOp())
	}
	m.pushCheckGen++
	m.statusMsg = "checking remote tags…"
	return m, m.pushTagCheckCmd(m.pushCheckGen, tipTags)
}

// pushTagsNoun returns "tag x" or "tags x, y" for error messages and prompts.
func pushTagsNoun(names []string) string {
	if len(names) == 1 {
		return "tag " + names[0]
	}
	return "tags " + strings.Join(names, ", ")
}
