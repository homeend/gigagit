package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// R7, R9: a PR diff's . menu offers Send to GitHub… and Verdict…; the old
// Send review… is gone, and a note's own rows are the one-note send only.
func TestPRDiffMenuOffersSendToGitHub(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t) // forge_send_menu_test.go: a PR diff with a local note
	m.diffLayer().setCursorLine(4, m.diffBodyRows())
	ids := menuIDs(m) // open_external_menu_test.go: the . menu's ids as a set
	for _, want := range []string{"pr-send", "pr-verdict", "note-send"} {
		if !ids[want] {
			t.Errorf("menu lacks %s: %v", want, ids)
		}
	}
	for _, gone := range []string{"pr-send-review", "note-send-review"} {
		if ids[gone] {
			t.Errorf("menu still has %s", gone)
		}
	}
	var label string
	for _, r := range availableActions(m) {
		if r.id == "pr-send" {
			label = r.label
		}
	}
	if label != "Send to GitHub…" {
		t.Fatalf("label %q", label)
	}
}

// The PR details' s opens the panel; v the verdict box; the hint line says so.
func TestPRHubSOpensThePanel(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openPRHub(m.prs[0])
	h := layerOf[*prHubPopup](m)
	if h == nil {
		t.Fatal("no hub")
	}
	if !strings.Contains(h.keys, "[s] send to GitHub") {
		t.Fatalf("keys %q", h.keys)
	}
	_, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if cmd == nil {
		t.Fatal("s must read the candidates")
	}
	m2, _ := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if layerOf[*verdictPopup](m2) == nil {
		t.Fatal("v opens the verdict box")
	}
}
