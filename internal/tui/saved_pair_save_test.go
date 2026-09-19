package tui

import (
	"context"
	"strings"
	"testing"
)

func savePairRowIDs(m Model) []string {
	var ids []string
	for _, r := range m.commitSavePairRows() {
		ids = append(ids, r.id)
	}
	return ids
}

func TestSavePairRowsGating(t *testing.T) {
	t.Parallel()
	base := loadedModelLinearCommits(t, 4)
	base.focus = panelCommits
	base.status = dirtyStatus()
	base.wipRows = deriveWipRows(base.status)
	base = base.rebuildCommitGraph()
	h := func(i int) string { return base.commits[i].Hash }
	for _, c := range []struct {
		name string
		set  map[string]bool
		want int
	}{
		{"none", nil, 0},
		{"one commit", map[string]bool{h(0): true}, 0},
		{"two commits", map[string]bool{h(0): true, h(2): true}, 2},
		{"three commits", map[string]bool{h(0): true, h(1): true, h(2): true}, 0},
		{"commit + working tree", map[string]bool{h(1): true, wipKey(wipRow{kind: wipWorktree}): true}, 0},
		{"commit + staged", map[string]bool{h(1): true, wipKey(wipRow{kind: wipStaged}): true}, 0},
	} {
		m := base
		m.commitCompareSet = c.set
		if got := savePairRowIDs(m); len(got) != c.want {
			t.Errorf("%s: rows = %v, want %d", c.name, got, c.want)
		}
	}
	// Two commits marked, but the Commits panel is not the focus: no rows.
	m := base
	m.commitCompareSet = map[string]bool{h(0): true, h(2): true}
	m.focus = panelBranches
	if got := savePairRowIDs(m); len(got) != 0 {
		t.Errorf("unfocused: rows = %v", got)
	}
}

// The saved direction must be the one "Compare selection" diffs in — older →
// newer by feed order — whatever order the marks were made in. Asserted
// against compareSelectionEndpoints on the SAME model so the two cannot drift.
func TestSavePairDirectionIsCompareSelections(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 4)
	m.focus = panelCommits
	m.svc.UsePreviewsDir(t.TempDir())
	older, newer := m.commits[3].Hash, m.commits[0].Hash
	m.commitCompareSet = map[string]bool{newer: true, older: true}
	left, right, _, ok := m.compareSelectionEndpoints()
	if !ok || left.Hash() != older || right.Hash() != newer {
		t.Fatalf("fixture: compare selection = %s..%s", left.Hash(), right.Hash())
	}
	rows := m.commitSavePairRows()
	if len(rows) != 2 || rows[0].id != "commit-save-pair" || rows[1].id != "commit-save-pair-reversed" {
		t.Fatalf("rows = %+v", rows)
	}
	u, cmd := rows[0].run(m)
	m = drainMsgs(t, u.(Model), cmd, 4)
	pairs, err := m.svc.PairList(context.Background())
	if err != nil || len(pairs) != 1 || pairs[0].A != older || pairs[0].B != newer {
		t.Fatalf("saved = %+v, %v; want %s..%s", pairs, err, older, newer)
	}
	if !strings.Contains(m.statusMsg, pairs[0].Label) {
		t.Fatalf("status = %q, want the saved label", m.statusMsg)
	}
	if len(m.commitCompareSet) != 2 {
		t.Fatal("saving must leave the marks alone")
	}
	if len(m.previews) != 1 || m.previews[0].kind != rowPair {
		t.Fatalf("the Previews rows must refresh with the pair: %+v", m.previews)
	}
	// The reversed row stores the swap as a SECOND entry.
	u, cmd = rows[1].run(m)
	m = drainMsgs(t, u.(Model), cmd, 4)
	pairs, _ = m.svc.PairList(context.Background())
	if len(pairs) != 2 || pairs[1].A != newer || pairs[1].B != older {
		t.Fatalf("reversed = %+v", pairs)
	}
	// Saving the same pair again says so instead of failing.
	u, cmd = rows[0].run(m)
	m = drainMsgs(t, u.(Model), cmd, 4)
	if got, _ := m.svc.PairList(context.Background()); len(got) != 2 || strings.Contains(m.statusMsg, "error") {
		t.Fatalf("duplicate save: %d pairs, status %q", len(got), m.statusMsg)
	}
}

func TestSavePairRowsAreInTheCommitsMenu(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true, m.commits[1].Hash: true}
	var ids []string
	for _, r := range m.appendCommitContextRows(nil) {
		ids = append(ids, r.id)
	}
	joined := strings.Join(ids, " ")
	i := strings.Index(joined, "commit-compare-selection")
	j := strings.Index(joined, "commit-save-pair")
	if i < 0 || j < i {
		t.Fatalf("save rows must follow Compare selection in the . menu: %v", ids)
	}
}
