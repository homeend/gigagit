package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// compareScope selects which subset of a branch-pair comparison's files is
// listed: everything, or only the files one branch touched since the two
// diverged (the origin sets from domain.CompareOrigins).
type compareScope int

const (
	compareScopeAll   compareScope = iota // every tip-to-tip difference
	compareScopeLeft                      // only files the left (marked) branch changed
	compareScopeRight                     // only files the right (selected) branch changed
)

// comparePairState is the branch-pair extension of the compare files view:
// present only when the comparison was opened from the Branches pair-op
// Compare row. It retains the raw tip-to-tip file list (so scope changes
// rebuild rows locally) and the async-loaded origin sets. Pointer field on
// Model (value receiver): mutations persist across the value copy.
type comparePairState struct {
	left, right   string             // full branch names (display + origin labels)
	files         []model.CommitFile // raw tip-to-tip compare list (retained for filtering)
	origins       model.CompareOrigins
	originsLoaded bool
	originsErr    error
	scope         compareScope
}

// compareOriginsMsg delivers the async origin-set load; tag gates staleness
// against m.compareTag (the compareFilesMsg convention).
type compareOriginsMsg struct {
	tag     string
	origins model.CompareOrigins
	err     error
}

// branchCompareTitle is the compare-view title for a branch pair, spelling
// out FULL branch names (Endpoint.Display truncates to 7 chars) and the
// active scope.
func branchCompareTitle(left, right string, scope compareScope) string {
	t := left + " ↔ " + right
	switch scope {
	case compareScopeLeft:
		t += " — only files " + left + " changed"
	case compareScopeRight:
		t += " — only files " + right + " changed"
	}
	return t
}

// openBranchCompare opens the compare files view for two branches (full
// tip-to-tip diff, marked = left/older, selected = right/newer), arms the
// branch-pair state, and starts the origin-set load in the background.
func (m Model) openBranchCompare(marked, selected string) (Model, tea.Cmd) {
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: marked}
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: selected}
	tag := "cmp:" + left.CacheTag() + ":" + right.CacheTag()
	// Same pair already showing: keep it (the openCompareFiles same-tag
	// convention), and keep its state — re-arming would drop loaded origins.
	if m.filesView != nil && m.inCompareMode() && m.compareTag == tag && m.comparePair != nil {
		return m, nil
	}
	var cmd tea.Cmd
	m, cmd = m.openCompareFiles(left, right) // clean slate: clears any prior comparePair
	m.comparePair = &comparePairState{left: marked, right: selected}
	m.filesTitle = branchCompareTitle(marked, selected, compareScopeAll)
	return m, tea.Batch(cmd, m.loadCompareOriginsCmd(marked, selected, tag))
}

// loadCompareOriginsCmd fetches the origin sets off the UI thread.
func (m Model) loadCompareOriginsCmd(a, b, tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		origins, err := svc.CompareOrigins(context.Background(), a, b)
		return compareOriginsMsg{tag: tag, origins: origins, err: err}
	}
}

// pathSet returns the active scope's origin set; nil means "no filtering"
// (compareScopeAll).
func (p *comparePairState) pathSet() map[string]bool {
	switch p.scope {
	case compareScopeLeft:
		return p.origins.APaths
	case compareScopeRight:
		return p.origins.BPaths
	}
	return nil
}

// filterCompareFiles keeps the rows whose path (or rename old-path) is in
// set; a nil set keeps everything.
func filterCompareFiles(files []model.CommitFile, set map[string]bool) []model.CommitFile {
	if set == nil {
		return files
	}
	out := make([]model.CommitFile, 0, len(files))
	for _, f := range files {
		if set[f.Path] || (f.OldPath != "" && set[f.OldPath]) {
			out = append(out, f)
		}
	}
	return out
}

// cycleCompareScope is the f key: advance the origin-filter scope and rebuild
// the tree rows from the retained raw list. Origins not usable yet: a status
// note, scope unchanged.
func (m Model) cycleCompareScope() Model {
	p := m.comparePair
	if p == nil {
		return m // not a branch-pair compare: f is inert
	}
	if p.originsErr != nil {
		if errors.Is(p.originsErr, domain.ErrNoMergeBase) {
			m.statusMsg = "no common ancestor — filter unavailable"
		} else {
			m.statusMsg = "origin filter unavailable: " + p.originsErr.Error()
		}
		return m
	}
	if !p.originsLoaded {
		m.statusMsg = "origin filter loading…"
		return m
	}
	p.scope = (p.scope + 1) % 3
	m.filesView.lines = commitFileLines(filterCompareFiles(p.files, p.pathSet()))
	m.filesView.sel = 0
	m.filesTitle = branchCompareTitle(p.left, p.right, p.scope)
	return m
}
