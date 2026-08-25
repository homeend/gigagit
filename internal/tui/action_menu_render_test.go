package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMenuRendersOverDiffView(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	m = m.pushLayer(&diffView{title: "a.go", rev: "abc123"})
	m = m.openActionMenu()
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Actions") {
		t.Fatalf("action menu must render over the diff view:\n%s", out)
	}
}

func TestMenuRendersOverHistory(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	m = m.pushLayer(newHistoryView(navContext{path: "a.go", rev: "abc"}))
	m = m.openActionMenu()
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Actions") {
		t.Fatalf("action menu must render over the history view:\n%s", out)
	}
}
