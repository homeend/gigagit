package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/notemeta"
)

// R9: the panel opens as before — nothing ticked, no meta on a collapsed
// row; → opens a row and shows one meta row: badges, the audience marker,
// the line entries.
func TestSendPanelExpandedMetaRow(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	c := panelCands()
	c.Groups[0].Rows[0].Meta = notemeta.Resolved{
		Badges:   []notemeta.Badge{{Key: "kind", Value: "issue", Color: "red", Style: notemeta.StyleFilled}},
		Audience: "author",
		Lines:    []notemeta.Line{{Label: "intent", Value: "checked the key"}},
	}
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: c})
	p := layerOf[*sendPanel](m)
	view := ansi.Strip(m.View())
	if strings.Contains(view, "checked the key") || strings.Contains(view, i18n.T("→ author")) {
		t.Fatal("a collapsed row shows no meta (R9)")
	}
	if p.tickedCount() != 0 {
		t.Fatal("nothing is ticked on open (R9)")
	}
	p.fold(true)
	view = ansi.Strip(m.View())
	want := " issue   " + i18n.T("→ author") + " · intent: checked the key"
	if !strings.Contains(view, want) {
		t.Fatalf("the opened row lacks %q:\n%s", want, view)
	}
	if p.tickedCount() != 0 {
		t.Fatal("opening a row ticks nothing")
	}
}

func TestSendMetaRun(t *testing.T) {
	t.Parallel()
	run, cls := sendMetaRun(notemeta.Resolved{Badges: []notemeta.Badge{{Key: "kind", Value: "issue", Color: "red", Style: notemeta.StyleFilled}},
		Lines: []notemeta.Line{{Label: "intent", Value: "x"}}})
	if run != " issue  · intent: x" || len(cls) != len([]rune(run)) || cls[1] != metaClass("red", notemeta.StyleFilled) {
		t.Fatalf("run = %q %v", run, cls)
	}
	if run, _ := sendMetaRun(notemeta.Resolved{Lines: []notemeta.Line{{Label: "intent", Value: "x"}}}); run != "intent: x" {
		t.Fatalf("lines only = %q", run)
	}
	if run, _ := sendMetaRun(notemeta.Resolved{}); run != "" {
		t.Fatal("no meta, no row")
	}
}
