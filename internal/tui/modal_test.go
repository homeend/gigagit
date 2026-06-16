package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/engine"
)

// The decision modal must overlay the centered box on the interface (like every
// other popup), not render standalone in the top-left corner.
func TestModalRendersCenteredOverInterface(t *testing.T) {
	m, _ := modalModel()
	m.width, m.height = 100, 30
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "diverged") {
		t.Fatal("modal prompt missing from render")
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 20 {
		t.Fatalf("modal should overlay the full-height interface, got %d lines:\n%s", len(lines), out)
	}
	if strings.HasPrefix(lines[0], "╔") {
		t.Errorf("modal renders at the top-left (its border on row 0), not centered:\n%s", out)
	}
}

func modalModel() (Model, chan engine.DecisionResponse) {
	m := New(nil)
	reply := make(chan engine.DecisionResponse, 1)
	m.modal = &decisionState{
		req:   engine.DecisionRequest{ID: "non-fast-forward", Prompt: "diverged", Options: []string{"rebase", "merge", "abort"}},
		reply: reply,
	}
	return m, reply
}

func TestModalEnterSendsSelectedOption(t *testing.T) {
	m, reply := modalModel()
	updated, _ := m.Update(keyMsg("down")) // → "merge"
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)

	if m.modal != nil {
		t.Fatal("modal should be cleared after a choice")
	}
	select {
	case resp := <-reply:
		if resp.Option != "merge" {
			t.Fatalf("option = %q, want merge", resp.Option)
		}
	default:
		t.Fatal("no response sent on the reply channel")
	}
}

func TestModalEscAborts(t *testing.T) {
	m, reply := modalModel()
	updated, _ := m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.modal != nil {
		t.Fatal("modal should be cleared after esc")
	}
	select {
	case resp := <-reply:
		if resp.Option != "abort" {
			t.Fatalf("esc option = %q, want abort", resp.Option)
		}
	default:
		t.Fatal("esc should send a response")
	}
}

func TestModalRendersPromptAndOptions(t *testing.T) {
	m, _ := modalModel()
	m.width, m.height = 80, 24
	out := m.View()
	if !strings.Contains(out, "diverged") {
		t.Fatalf("modal view missing prompt:\n%s", out)
	}
	for _, opt := range []string{"rebase", "merge", "abort"} {
		if !strings.Contains(out, opt) {
			t.Fatalf("modal view missing option %q:\n%s", opt, out)
		}
	}
}
