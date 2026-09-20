package tui

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// savedPairModel is mergePreviewModel plus one saved commit pair main..feat/x, so
// the Previews tab holds BOTH kinds: row 0 the merge preview, row 1 the pair.
func savedPairModel(t *testing.T) (Model, domain.CommitPair) {
	t.Helper()
	m, _, _ := mergePreviewModel(t)
	p, err := m.svc.PairAdd(context.Background(), "main", "feat/x", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	m = m.activateTab(panelPreviews)
	m.width, m.height = 160, 40
	return m, p
}

func TestPreviewsTabListsBothKinds(t *testing.T) {
	t.Parallel()
	m, p := savedPairModel(t)
	if len(m.previews) != 2 || m.previews[0].kind != rowMerge || m.previews[1].kind != rowPair {
		t.Fatalf("rows = %+v", m.previews)
	}
	out := m.View()
	subject := p.A[:7] + ".." + p.B[:7]
	// main..feat/x TWO-dot: a.txt only (main has not moved in this fixture).
	if !strings.Contains(out, "attempt") || !strings.Contains(out, subject) || !strings.Contains(out, "1 file") {
		t.Fatalf("the pair row must show label, <a7>..<b7> and the file count:\n%s", out)
	}
	if !strings.Contains(out, "feat/x → main") {
		t.Fatalf("the merge preview row must survive beside it:\n%s", out)
	}
	l := previewList{rows: m.previews}
	if l.Key(1) != p.ID || l.Name(1) != "attempt" || l.Date(1) != p.Created.Unix() {
		t.Fatalf("list identity of the pair row = %q %q %d", l.Key(1), l.Name(1), l.Date(1))
	}
}

func TestPairRowMissingCommitStateAndEnterRefusal(t *testing.T) {
	t.Parallel()
	m, p := savedPairModel(t)
	r := previewRow{kind: rowPair, pair: p, psum: domain.PairSummary{State: domain.PairMissingB}}
	if got := previewStateText(r); !strings.Contains(got, p.B[:7]) {
		t.Fatalf("state text = %q, want the missing short sha", got)
	}
	m, cmd := m.openPairRow(r)
	if cmd != nil || !strings.Contains(m.statusMsg, p.B[:7]) {
		t.Fatalf("a gone commit must refuse without a git call: cmd=%v status=%q", cmd != nil, m.statusMsg)
	}
}

func TestEnterOnPairRowOpensACommitComparisonWithoutPreviewState(t *testing.T) {
	t.Parallel()
	m, p := savedPairModel(t)
	m.sel[panelPreviews] = 1
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 6)
	if m.filesView == nil {
		t.Fatalf("enter must open the files view (status %q)", m.statusMsg)
	}
	if m.filesTitle != pairTitle("attempt") {
		t.Fatalf("title = %q", m.filesTitle)
	}
	// A frozen pair is NOT a merge preview: nothing follows tips. Its note
	// scope is a PAIR scope (saved_pair_notes_test.go covers it).
	if m.previewOpen != nil {
		t.Fatalf("pair armed previewOpen: %v", m.previewOpen)
	}
	if set := m.filesPreviewSet; set != nil && !set.IsPair() {
		t.Fatalf("a pair must never carry a branch-pair scope: %+v", set)
	}
	if !strings.Contains(m.compareTag, p.A) || !strings.Contains(m.compareTag, p.B) {
		t.Fatalf("compare tag %q must name both frozen shas", m.compareTag)
	}
}

func TestStalePairOpenIsDropped(t *testing.T) {
	t.Parallel()
	m, p := savedPairModel(t)
	eps, err := m.svc.PairOpen(context.Background(), p.A, p.B)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.handlePairOpenMsg(pairOpenMsg{pair: p, eps: eps, gen: m.previewGen - 1})
	if m.filesView != nil {
		t.Fatal("a pair open stamped with an old generation must be dropped")
	}
}

func TestPairRowRenameSwapRemove(t *testing.T) {
	t.Parallel()
	m, p := savedPairModel(t)
	m.sel[panelPreviews] = 1
	updated, _ := m.Update(keyMsg("e"))
	m = updated.(Model)
	pop := layerOf[*previewRenamePopup](m)
	if pop == nil || pop.kind != rowPair || pop.id != p.ID {
		t.Fatalf("e must open rename carrying the pair kind: %+v", pop)
	}
	m = typeString(t, m, " 2")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if m.statusMsg != "" && strings.Contains(m.statusMsg, "error") {
		t.Fatalf("rename: %s", m.statusMsg)
	}
	if got, _ := m.svc.PairGet(context.Background(), p.ID); got.Label != "attempt 2" {
		t.Fatalf("label = %q", got.Label)
	}
	// s saves the REVERSED pair as a second entry.
	m.sel[panelPreviews] = 1
	updated, cmd = m.Update(keyMsg("s"))
	m = drainMsgs(t, updated.(Model), cmd, 4)
	pairs, _ := m.svc.PairList(context.Background())
	if len(pairs) != 2 || pairs[1].A != p.B || pairs[1].B != p.A {
		t.Fatalf("s must save b..a: %+v", pairs)
	}
	// d removes the pair, never the merge preview beside it.
	for i, r := range m.previews {
		if r.id() == p.ID {
			m.sel[panelPreviews] = i
		}
	}
	updated, _ = m.Update(keyMsg("d"))
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("d must confirm")
	}
	updated, cmd = m.modal.onResolve(m, "Remove")
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if strings.Contains(m.statusMsg, "error") {
		t.Fatalf("remove: %s", m.statusMsg)
	}
	if _, err := m.svc.PairGet(context.Background(), p.ID); err == nil {
		t.Fatal("the pair survived its remove")
	}
	if ps, _ := m.svc.PreviewList(context.Background()); len(ps) != 1 {
		t.Fatalf("the merge preview must survive: %+v", ps)
	}
}

func TestPairRowCopiesTheBareFullShaPairLink(t *testing.T) {
	t.Parallel()
	m, p := savedPairModel(t)
	m.sel[panelPreviews] = 1
	r, ok := m.selectedPreview()
	if !ok || r.kind != rowPair {
		t.Fatalf("selected = %+v", r)
	}
	link, ok := m.pairLinkFor(r.pair.A, r.pair.B)
	if !ok || !strings.HasSuffix(link, "@"+p.A+".."+p.B) || !strings.HasPrefix(link, "gg://") {
		t.Fatalf("link = %q", link)
	}
	// It is the text the STORE holds, so a copied link and `gg compare --list`
	// agree on one spelling.
	all, _ := m.svc.SavedCompareList(context.Background())
	if all[len(all)-1].Left != link {
		t.Fatalf("copied %q, stored %q", link, all[len(all)-1].Left)
	}
}

// The ROW's copy carries the landing hint on top of that stored text; the
// bare builder above stays what the store and `gg compare --list` spell.
func TestPairRowCopyCarriesThePreviewHint(t *testing.T) {
	t.Parallel()
	m, p := savedPairModel(t)
	m = m.activateTab(panelPreviews)
	m.sel[panelPreviews] = 1
	got, ok := m.contextLinkText()
	if !ok || !strings.HasSuffix(got, "@"+p.A+".."+p.B+"?preview="+p.ID) {
		t.Fatalf("pair row link = %q,%v; want the pair link + ?preview=%s", got, ok, p.ID)
	}
}

// previewRow.rec is ZERO on a pair row, so a consumer that reads it directly
// hands empty branch names to a merge-preview path. Every reader outside the
// row's own file must go through merge(), whose ok forces the question.
func TestPreviewRowRecIsReadOnlyInItsOwnFile(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "rec" {
				return true
			}
			seen++
			if name != "preview_panel.go" {
				t.Errorf("%s: reads .rec directly — use previewRow.merge()/id()/label()", fset.Position(sel.Pos()))
			}
			return true
		})
	}
	if seen == 0 {
		t.Fatal("the scan saw no .rec selector at all: it cannot see its subject")
	}
}
