package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// versionHintLoadedMsg carries a ?version= hint's lookup back to the UI
// thread: the record's branch and that branch's rows (newest first), with
// idx the record's row. found=false is a miss, err a failed read.
type versionHintLoadedMsg struct {
	gen    int
	branch string
	rows   []model.BranchVersion
	idx    int
	found  bool
	err    error
}

// loadVersionForHintCmd runs domain.FindVersion (one for-each-ref) plus the
// branch's row list off the Update thread, stamped with the hint generation.
func (m Model) loadVersionForHintCmd(c steer.Command, gen int) tea.Cmd {
	svc := m.svc
	id := c.HintID
	var a, b string
	if c.Target != nil {
		a, b = c.Target.A, c.Target.B
	}
	return func() tea.Msg {
		if svc == nil {
			return versionHintLoadedMsg{gen: gen}
		}
		ctx := context.Background()
		branch, v, ok, err := svc.FindVersion(ctx, id, a, b)
		if err != nil || !ok {
			return versionHintLoadedMsg{gen: gen, err: err}
		}
		rows, err := svc.BranchVersions(ctx, branch)
		if err != nil {
			return versionHintLoadedMsg{gen: gen, err: err}
		}
		idx := -1
		for i := range rows {
			if rows[i].Ref == v.Ref {
				idx = i
				break
			}
		}
		if idx < 0 {
			return versionHintLoadedMsg{gen: gen}
		}
		return versionHintLoadedMsg{gen: gen, branch: branch, rows: rows, idx: idx, found: true}
	}
}

// versionHintLoaded honours the hint once its lookup lands: the Branch
// versions popup on that row, PARKED under the compare the link opened (esc
// on the compare restores it — "popup → enter" in reverse), or pushed live
// when the compare has since closed. A miss is a notice; a stale generation
// is dropped, a failed read is its own notice. The hint degrades, it never
// fails.
func (m Model) versionHintLoaded(msg versionHintLoadedMsg) (Model, tea.Cmd) {
	ph := m.pendingHint
	if ph == nil || ph.cmd.HintKind != "version" || msg.gen != ph.tag {
		return m, nil
	}
	m.pendingHint = nil
	if msg.err != nil {
		// A failed read is not a miss: it must not claim the version is
		// unrecorded here.
		m.statusMsg = i18n.T("could not look up version %s; the link still landed", ph.cmd.HintID)
		return m, nil
	}
	if !msg.found {
		m.statusMsg = i18n.T("version %s is not recorded here; the link still landed", ph.cmd.HintID)
		return m, nil
	}
	p := &versionsPopup{mode: versionsModeVersions, branch: msg.branch, rows: msg.rows, sel: msg.idx}
	if m.filesView != nil {
		m.filesReturnLayers = []layer{p} // the latest opener wins
		return m, nil
	}
	return m.pushLayer(p), nil
}
