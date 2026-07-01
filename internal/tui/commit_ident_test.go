package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

func TestCommitIdentOfTipPrefersHead(t *testing.T) {
	c := model.Commit{Refs: []model.Ref{
		{Name: "feat", Kind: model.RefLocal},
		{Name: "main", Kind: model.RefLocal, Head: true},
	}, Source: "feat"}
	id := commitIdentOf(c, nil)
	if !id.tip || id.name != "main" || !id.head {
		t.Fatalf("ident = %+v, want tip main (head)", id)
	}
	if len(id.extra) != 1 || id.extra[0] != "feat" {
		t.Fatalf("extra = %v, want [feat]", id.extra)
	}
	if id.label() != "*main" {
		t.Fatalf("label = %q, want *main", id.label())
	}
}

func TestCommitIdentOfLineageUsesSource(t *testing.T) {
	c := model.Commit{Source: "feature/long"} // no refs
	id := commitIdentOf(c, nil)
	if id.tip || id.name != "feature/long" || id.head {
		t.Fatalf("ident = %+v, want lineage feature/long", id)
	}
	if id.label() != "feature/long" {
		t.Fatalf("label = %q", id.label())
	}
}

func TestCommitIdentOfNoneIsBlank(t *testing.T) {
	id := commitIdentOf(model.Commit{}, nil) // no refs, no source
	if id.name != "" || id.label() != "" {
		t.Fatalf("ident = %+v, want blank", id)
	}
}

func TestCommitIdentTokenTrimsLongName(t *testing.T) {
	id := commitIdent{name: "b/from-feat-cherry-pick-very-long", tip: true}
	tok, trimmed := id.token(commitIdentW)
	if !trimmed {
		t.Fatal("a >16 name must report trimmed")
	}
	if lipgloss.Width(tok) != commitMarkerW+commitIdentW {
		t.Fatalf("token width = %d, want %d", lipgloss.Width(tok), commitMarkerW+commitIdentW)
	}
	if !strings.HasSuffix(tok, "…") {
		t.Fatalf("trimmed token must end with …: %q", tok)
	}
}

func TestCommitIdentTokenPadsShortName(t *testing.T) {
	id := commitIdent{name: "main", tip: true}
	tok, trimmed := id.token(commitIdentW)
	if trimmed {
		t.Fatal("a short name must not be trimmed")
	}
	if lipgloss.Width(tok) != commitMarkerW+commitIdentW {
		t.Fatalf("token width = %d, want %d (padded)", lipgloss.Width(tok), commitMarkerW+commitIdentW)
	}
	if !strings.Contains(tok, "main") {
		t.Fatalf("token = %q, want main somewhere in token", tok)
	}
}

// The commit list stays searchable by the FULL commit id and the FULL branch
// name even though neither appears verbatim in the trimmed, id-less row.
func TestCommitFilterSearchesHiddenHashAndFullName(t *testing.T) {
	m := footerModel()
	long := "feature/some-very-long-branch-name" // > commitIdentW → trimmed in the row
	m.commits = []model.Commit{
		{Hash: "deadbeefcafe", Subject: "alpha", Refs: []model.Ref{{Name: long, Kind: model.RefLocal}}},
		{Hash: "0000111122", Subject: "beta"},
	}
	m.filterPanel = panelCommits

	m.filterQuery = "deadbeef" // a hash prefix shown in no row
	if _, idx := m.panelView(panelCommits); len(idx) != 1 || idx[0] != 0 {
		t.Fatalf("hash-prefix filter idx = %v, want [0]", idx)
	}
	m.filterQuery = "very-long-branch" // tail of the trimmed-away branch name
	if _, idx := m.panelView(panelCommits); len(idx) != 1 || idx[0] != 0 {
		t.Fatalf("full-name filter idx = %v, want [0]", idx)
	}
}

// End-to-end: a lineage row's branch name is dimmed in the assembled panel; a
// tip row is not.
func TestRenderPanelDimsLineageName(t *testing.T) {
	forceColor(t)
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{
		{Hash: "aaaa", Subject: "tip", Refs: []model.Ref{{Name: "main", Kind: model.RefLocal, Head: true}}},
		{Hash: "bbbb", Subject: "lineage", Source: "main"},
	}
	m.sel[panelCommits] = 0 // select the tip → the lineage row (1) gets decorated
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	out := m.renderPanel(panelCommits, "Commits", rows, decos, 40, 8)
	probe := dimIdentStyle.Render("x")
	esc := probe[:strings.IndexRune(probe, 'x')] // the leading dim escape
	if esc == "" || !strings.Contains(out, esc) {
		t.Fatalf("lineage branch name must be dimmed (escape %q):\n%s", esc, out)
	}
}

// The decorator dims the identity range and colors the dot in one pass, width
// preserved.
func TestCommitLineDecoratorDimsIdentAndColorsDot(t *testing.T) {
	forceColor(t)
	// visible: "  ● main            subject" — prefix(2) + ●(col2) + space + ident
	visible := "  ● " + padRight("main", commitIdentW) + " subject"
	deco := commitLineDecorator(true, 2, laneColor(0), true, 4, commitIdentW, nil)
	out := deco(visible, 0, 0)
	if lipgloss.Width(out) != lipgloss.Width(visible) {
		t.Fatalf("decorator changed width: %d → %d", lipgloss.Width(visible), lipgloss.Width(out))
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("decorator emitted no ANSI styling")
	}
	// continuation lines untouched
	if got := deco(visible, 0, 1); got != visible {
		t.Fatal("decorator must be a no-op on wrap continuation lines")
	}
}

func TestCommitIdentOfInSyncTipMarksBoth(t *testing.T) {
	tracked := map[string]string{"origin/main": "main"}
	c := model.Commit{Refs: []model.Ref{
		{Name: "main", Kind: model.RefLocal, Head: true},
		{Name: "origin/main", Kind: model.RefRemote},
	}}
	id := commitIdentOf(c, tracked)
	if !id.tip || !id.remoteTip || id.name != "main" || !id.head {
		t.Fatalf("ident = %+v, want local+remote tip main (head)", id)
	}
}

func TestCommitIdentOfRemoteOnlyTipUsesBranchName(t *testing.T) {
	tracked := map[string]string{"origin/main": "main"}
	// A commit decorated only by the tracked remote ref (local branch is behind).
	c := model.Commit{Refs: []model.Ref{{Name: "origin/main", Kind: model.RefRemote}}, Source: "main"}
	id := commitIdentOf(c, tracked)
	if id.tip || !id.remoteTip || id.name != "main" {
		t.Fatalf("ident = %+v, want remote-only tip named main", id)
	}
}

func TestCommitIdentOfUntrackedRemoteIsNotMarked(t *testing.T) {
	// origin/feature is not any local branch's upstream → no remote-tip marker.
	c := model.Commit{Refs: []model.Ref{{Name: "origin/feature", Kind: model.RefRemote}}, Source: "main"}
	id := commitIdentOf(c, map[string]string{"origin/main": "main"})
	if id.remoteTip {
		t.Fatalf("ident = %+v, want no remoteTip for an untracked remote", id)
	}
	if id.name != "main" { // falls back to lineage source
		t.Fatalf("name = %q, want lineage main", id.name)
	}
}

func TestCommitIdentMarkers(t *testing.T) {
	cases := []struct {
		name string
		id   commitIdent
		want string
	}{
		{"in sync", commitIdent{tip: true, remoteTip: true}, "↓↑"},
		{"local only", commitIdent{tip: true}, "↓ "},
		{"remote only", commitIdent{remoteTip: true}, "↑ "},
		{"neither", commitIdent{}, "  "},
	}
	for _, tc := range cases {
		if got := tc.id.markers(); got != tc.want {
			t.Errorf("%s: markers() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCommitIdentTokenIncludesMarkerPrefix(t *testing.T) {
	id := commitIdent{name: "main", tip: true, remoteTip: true}
	tok, trimmed := id.token(commitIdentW)
	if trimmed {
		t.Fatal("a short name must not be trimmed")
	}
	if !strings.HasPrefix(tok, "↓↑ ") {
		t.Fatalf("token = %q, want it to start with the marker prefix", tok)
	}
	if want := commitMarkerW + commitIdentW; lipgloss.Width(tok) != want {
		t.Fatalf("token width = %d, want %d", lipgloss.Width(tok), want)
	}
}

// Single-marker rows must be the SAME width as two-marker rows, else rows with
// one marker misalign against rows with two. Pins the left-pack field at 2 cells.
func TestCommitIdentTokenSingleMarkerWidth(t *testing.T) {
	for _, id := range []commitIdent{
		{name: "main", tip: true},       // ↓  (local only)
		{name: "main", remoteTip: true}, // ↑  (remote only)
	} {
		tok, _ := id.token(commitIdentW)
		if want := commitMarkerW + commitIdentW; lipgloss.Width(tok) != want {
			t.Fatalf("token %q width = %d, want %d", tok, lipgloss.Width(tok), want)
		}
	}
}
