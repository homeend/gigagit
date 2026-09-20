package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func typeKeysPR(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		nm, _ := m.Update(keyMsg(k))
		m = nm.(Model)
	}
	return m
}

func searchAnswer(q domain.PRQuery) prSearchMsg {
	return prSearchMsg{q: q, res: domain.PRSearchResult{Query: q, PRs: []model.PullRequest{
		{Number: 12, Title: "Old work", Author: "hubot", State: model.PRStateMerged, URL: "https://github.com/o/r/pull/12"},
		{Number: 3, Title: "Dropped", Author: "ana", State: model.PRStateClosed},
	}}}
}

// searched opens the popup, runs a search for "old" and lands its answer.
func searched(t *testing.T, m Model) Model {
	t.Helper()
	m = typeKeysPR(t, m, "A", "o", "l", "d")
	nm, cmd := m.Update(keyMsg("enter"))
	m = nm.(Model)
	p := layerOf[*prSearchPopup](m)
	if p == nil || cmd == nil || !p.busy {
		t.Fatalf("enter must start the search (popup=%v cmd=%v)", p != nil, cmd != nil)
	}
	nm, _ = m.Update(searchAnswer(p.asked))
	return nm.(Model)
}

func TestPRSearchOpensOnlyFromThePRsTab(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	if m = typeKeysPR(t, m, "A"); layerOf[*prSearchPopup](m) == nil {
		t.Fatal("A on the Pull requests tab must open the search")
	}
	other := loadedModel(t) // Branches focused, no forge
	if other = typeKeysPR(t, other, "A"); other.topLayer() != nil {
		t.Fatal("A outside the Pull requests tab must do nothing")
	}
	noForge := prModel(t)
	noForge.forgeShown = false
	if noForge = typeKeysPR(t, noForge, "A"); noForge.topLayer() != nil {
		t.Fatal("no forge, no search")
	}
}

func TestPRSearchPromptZone(t *testing.T) {
	t.Parallel()
	m := typeKeysPR(t, prModel(t), "A")
	p := layerOf[*prSearchPopup](m)
	var seen []string
	for range 5 {
		seen = append(seen, p.state)
		m = typeKeysPR(t, m, "tab")
	}
	if got := strings.Join(seen, " "); got != "all closed merged open all" {
		t.Fatalf("state cycle = %q", got)
	}
	// Every printable key is query text: nothing global fires under the popup.
	m = typeKeysPR(t, m, "p", "S", "q", "A", "i")
	if p.input.Value() != "pSqAi" || m.topLayer() != p || m.stashView != nil {
		t.Fatalf("text = %q, top = %T", p.input.Value(), m.topLayer())
	}
	// ↓ has nowhere to go before an answer.
	if m = typeKeysPR(t, m, "down"); p.inRows {
		t.Fatal("↓ entered an empty results zone")
	}
	nm, cmd := m.Update(keyMsg("enter"))
	m = nm.(Model)
	// The loop above left the chip one step past "all".
	want := domain.PRQuery{State: domain.PRStateFilterClosed, Text: "pSqAi", Limit: domain.PRSearchDefaultLimit}
	if cmd == nil || !p.busy || p.asked != want {
		t.Fatalf("asked = %+v busy=%v", p.asked, p.busy)
	}
	if !strings.Contains(p.box(m), "searching…") {
		t.Errorf("no searching line:\n%s", p.box(m))
	}
	if m = typeKeysPR(t, m, "esc"); m.topLayer() != nil {
		t.Fatal("esc in the prompt must close the popup")
	}
}

func TestPRSearchAnswerGate(t *testing.T) {
	t.Parallel()
	m := typeKeysPR(t, prModel(t), "A", "x")
	nm, _ := m.Update(keyMsg("enter"))
	m = nm.(Model)
	p := layerOf[*prSearchPopup](m)
	// An answer to another query is dropped.
	stale := p.asked
	stale.Text = "older"
	nm, _ = m.Update(searchAnswer(stale))
	m = nm.(Model)
	if p.has || !p.busy {
		t.Fatal("a stale answer landed")
	}
	nm, _ = m.Update(searchAnswer(p.asked))
	m = nm.(Model)
	if !p.has || p.busy || !p.inRows || p.sel != 0 {
		t.Fatalf("answer: has=%v busy=%v inRows=%v", p.has, p.busy, p.inRows)
	}
	box := p.box(m)
	for _, want := range []string{"#12", "merged", "Old work", "#3", "closed", "Dropped", "> "} {
		if !strings.Contains(box, want) {
			t.Errorf("box lacks %q:\n%s", want, box)
		}
	}
	// A failure keeps the rows and says why.
	m = typeKeysPR(t, m, "esc")
	nm, _ = m.Update(keyMsg("enter"))
	m = nm.(Model)
	nm, _ = m.Update(prSearchMsg{q: p.asked, err: errors.New("HTTP 502\nmore")})
	m = nm.(Model)
	if box := p.box(m); !strings.Contains(box, "HTTP 502") || !strings.Contains(box, "Old work") || strings.Contains(box, "more\n") && p.busy {
		t.Errorf("after a failure:\n%s", box)
	}
	// An answer to a closed popup is dropped without a crash.
	m = typeKeysPR(t, m, "esc")
	if _, cmd := m.Update(searchAnswer(p.asked)); cmd != nil || m.topLayer() != nil {
		t.Fatal("a closed popup took an answer")
	}
}

func TestPRSearchRowsZone(t *testing.T) {
	t.Parallel()
	m := searched(t, prModel(t))
	p := layerOf[*prSearchPopup](m)
	m = typeKeysPR(t, m, "down", "down", "j")
	if p.sel != 1 {
		t.Fatalf("sel = %d, want 1 (clamped)", p.sel)
	}
	m = typeKeysPR(t, m, "up", "up")
	if p.inRows || p.sel != 0 {
		t.Fatalf("↑ on the first row must return to the prompt (inRows=%v sel=%d)", p.inRows, p.sel)
	}
	m = typeKeysPR(t, m, "down")
	if !p.inRows {
		t.Fatal("↓ must re-enter the rows")
	}
	if _, cmd := m.Update(keyMsg("y")); cmd == nil {
		t.Fatal("y must copy the URL")
	}
	// A global key does nothing here, and is not query text either.
	before := p.input.Value()
	m = typeKeysPR(t, m, "S", "p")
	if m.topLayer() != p || m.stashView != nil || p.input.Value() != before {
		t.Fatal("a key leaked out of the rows zone")
	}
	m = typeKeysPR(t, m, "esc")
	if p.inRows || m.topLayer() != p {
		t.Fatal("esc in the rows must return to the prompt, not close")
	}
	if m = typeKeysPR(t, m, "esc"); m.topLayer() != nil {
		t.Fatal("the second esc closes")
	}
}

func TestPRSearchHubReturnsToTheResults(t *testing.T) {
	t.Parallel()
	m := searched(t, prModel(t))
	p := layerOf[*prSearchPopup](m)
	nm, cmd := m.Update(keyMsg("i"))
	m = nm.(Model)
	hub := layerOf[*prHubPopup](m)
	if hub == nil || cmd == nil || hub.pr.Number != 12 {
		t.Fatalf("i must open the hub of the selected result (hub=%+v)", hub)
	}
	m = typeKeysPR(t, m, "esc")
	if m.topLayer() != p || !p.inRows {
		t.Fatalf("closing the hub must reveal the results (top=%T)", m.topLayer())
	}
}

func TestPRSearchMoreAndEmpty(t *testing.T) {
	t.Parallel()
	m := typeKeysPR(t, prModel(t), "A")
	p := layerOf[*prSearchPopup](m)
	nm, _ := m.Update(keyMsg("enter"))
	m = nm.(Model)
	ans := searchAnswer(p.asked)
	ans.res.More = true
	nm, _ = m.Update(ans)
	m = nm.(Model)
	if !strings.Contains(p.box(m), "more results — narrow the search") {
		t.Errorf("no more line:\n%s", p.box(m))
	}
	m = typeKeysPR(t, m, "esc")
	nm, _ = m.Update(keyMsg("enter"))
	m = nm.(Model)
	nm, _ = m.Update(prSearchMsg{q: p.asked, res: domain.PRSearchResult{Query: p.asked}})
	m = nm.(Model)
	if box := p.box(m); !strings.Contains(box, "no pull requests match") || p.inRows {
		t.Errorf("empty answer (inRows=%v):\n%s", p.inRows, box)
	}
}

func TestPRSearchFits(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{140, 40}, {60, 16}, {40, 10}} {
		m := prModel(t)
		m.width, m.height = size[0], size[1]
		m = typeKeysPR(t, m, "A")
		p := layerOf[*prSearchPopup](m)
		nm, _ := m.Update(keyMsg("enter"))
		m = nm.(Model)
		ans := searchAnswer(p.asked)
		for i := range 30 {
			ans.res.PRs = append(ans.res.PRs, model.PullRequest{Number: 100 + i,
				Title: strings.Repeat("a very long title ", 6), State: model.PRStateClosed})
		}
		ans.res.More = true
		nm, _ = m.Update(ans)
		m = nm.(Model)
		lines := strings.Split(strings.TrimRight(m.View(), "\n"), "\n")
		if len(lines) > m.height {
			t.Errorf("%v: %d lines > height", size, len(lines))
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > m.width {
				t.Errorf("%v: line width %d > %d: %q", size, w, m.width, l)
			}
		}
	}
}

func TestPRRowsForMatchesTheTab(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	if got, want := strings.Join(prRowsFor(m.prs), "\n"), strings.Join(m.prRows(), "\n"); got != want {
		t.Errorf("row painter drifted:\n%s\n--\n%s", got, want)
	}
}

// The PR diff is a files view, not a layer: opened from the results it parks
// the popup, and its esc brings the results back.
func TestPRSearchDiffParksThePopup(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m.forgeShown, m.forgeProvider, m.prs, m.prsLoaded = true, "github", testPRs(), true
	m = m.activateTab(panelPRs)
	m = searched(t, m)
	p := layerOf[*prSearchPopup](m)
	if _, cmd := m.Update(keyMsg("enter")); cmd == nil {
		t.Fatal("enter on a result must start the open")
	}
	if m.topLayer() != p {
		t.Fatal("the popup must stay up while the head is fetched")
	}
	eps, err := m.svc.PreviewOpen(context.Background(), "feat/x", "main")
	if err != nil {
		t.Fatal(err)
	}
	open := previewOpenMsg{source: "feat/x", target: "main", gen: m.previewGen, eps: eps, title: "PR #12 · Old work"}
	nm, _ := m.Update(open)
	m = nm.(Model)
	if m.filesView == nil || m.topLayer() != nil || len(m.filesReturnLayers) != 1 || m.filesReturnLayers[0] != layer(p) {
		t.Fatalf("the diff must park the popup (view=%v top=%T parked=%d)", m.filesView != nil, m.topLayer(), len(m.filesReturnLayers))
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	if m.topLayer() != p || !p.inRows || p.sel != 0 {
		t.Fatalf("closing the diff must bring the results back (top=%T)", m.topLayer())
	}

	// Without the search popup nothing is parked — another layer on top of a
	// re-opening preview must stay where the user is reading it.
	m2, _, _ := mergePreviewModel(t)
	m2 = m2.pushLayer(newContentPopup("x", nil))
	nm, _ = m2.Update(previewOpenMsg{source: "feat/x", target: "main", gen: m2.previewGen, eps: eps})
	m2 = nm.(Model)
	if len(m2.filesReturnLayers) != 0 || m2.topLayer() == nil {
		t.Fatalf("an unrelated layer was parked (parked=%d top=%T)", len(m2.filesReturnLayers), m2.topLayer())
	}
}
