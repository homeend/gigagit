package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
)

// runExportRow runs the overview-export row and returns its resolved message.
func runExportRow(t *testing.T, m Model, rows []actionRow) patchResolvedMsg {
	t.Helper()
	row, ok := rowByID(rows, "overview-export")
	if !ok || row.label != i18n.T("Export as HTML") {
		t.Fatalf("no export row in %d rows", len(rows))
	}
	_, cmd := row.run(m)
	if cmd == nil {
		t.Fatal("the row ran no command")
	}
	msg, ok := cmd().(patchResolvedMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	return msg
}

func TestOverviewMenuExportsAnAgentOverview(t *testing.T) {
	t.Parallel()
	m, _, _ := tourModel(t)
	msg := runExportRow(t, m, m.overviewRows())
	if msg.err != nil || msg.title != i18n.T("Export overview as HTML") ||
		filepath.Base(msg.defaultPath) != "tour.html" || !strings.Contains(string(msg.data), `id="gg-data"`) {
		t.Fatalf("msg = err %v title %q path %q", msg.err, msg.title, msg.defaultPath)
	}
	nm, _ := m.Update(msg)
	got := nm.(Model)
	p, ok := got.topLayer().(*exportPatchPopup)
	if !ok || !strings.Contains(p.render(got, ""), i18n.T("Export overview as HTML")) {
		t.Fatalf("top layer = %T", got.topLayer())
	}
}

func TestOverviewMenuExportsAStoredOverview(t *testing.T) {
	t.Parallel()
	m, _ := reviewSplitModel(t)
	msg := runExportRow(t, m, m.overviewRows())
	if msg.err != nil || !strings.HasPrefix(filepath.Base(msg.defaultPath), "review-overview-") {
		t.Fatalf("msg = err %v path %q", msg.err, msg.defaultPath)
	}
}

// The review view's ≡ Overview row offers the export in its . menu (the
// overview need not be opened first); a file row of the same tree does not.
func TestReviewOverviewRowOffersExport(t *testing.T) {
	t.Parallel()
	m, sha := reviewWithOverview(t)
	m = selectRow(t, m, func(l contentLine) bool { return l.overviewDoc })
	msg := runExportRow(t, m, availableActions(m))
	if msg.err != nil || filepath.Base(msg.defaultPath) != "review-overview-"+sha[:7]+".html" || !strings.Contains(string(msg.data), `"path":"a.go"`) {
		t.Fatalf("msg = err %v path %q", msg.err, msg.defaultPath)
	}
	m = selectRow(t, m, func(l contentLine) bool { return l.path == "a.go" })
	if _, ok := rowByID(availableActions(m), "overview-export"); ok {
		t.Fatal("a file row must not offer the overview export")
	}
}

// An open permanent overview's split exports by its overview id: the title
// is the overview's, the file the commit's.
func TestOverviewMenuExportsAPermanentOverview(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	msg := runExportRow(t, m, m.overviewRows())
	if msg.err != nil || filepath.Base(msg.defaultPath) != "overview-tour-of-a.html" || !strings.Contains(string(msg.data), "func A() { _ = 1 }") {
		t.Fatalf("msg = err %v path %q", msg.err, msg.defaultPath)
	}
}
