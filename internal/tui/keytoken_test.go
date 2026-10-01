package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// What the recorder writes is exactly what the headless driver accepts.
func TestKeyTokenRoundTrip(t *testing.T) {
	t.Parallel()
	toks := []string{"enter", "esc", "space", "tab", "up", "down", "left", "right",
		"bspace", "delete", "home", "end", "pgup", "pgdown",
		"C-t", "C-g", "C-c", "C-\\", "C-]", "M-a", "M-t", "M-down",
		"a", ".", "?", "#", "D", " "}
	for _, tok := range toks {
		msg, err := keyMsgFor(tok)
		if err != nil {
			t.Errorf("keyMsgFor(%q): %v", tok, err)
			continue
		}
		back, ok := keyToken(msg)
		if !ok || back != tok {
			t.Errorf("round trip %q → %#v → %q (ok=%v)", tok, msg, back, ok)
		}
	}
}

func TestKeyMsgForRejectsDiagnostics(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"<f1>", "", "C-", "M-"} {
		if _, err := keyMsgFor(bad); err == nil {
			t.Errorf("keyMsgFor(%q) must fail", bad)
		}
	}
}

// space is KeySpace carrying the rune, as Bubble Tea delivers it.
func TestSpaceCarriesItsRune(t *testing.T) {
	t.Parallel()
	msg, _ := keyMsgFor("space")
	if msg.Type != tea.KeySpace || string(msg.Runes) != " " {
		t.Fatalf("space = %#v", msg)
	}
}

func TestSplitLiteral(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{"foo": {"f", "o", "o"}, "enter": {"enter"}, "C-t": {"C-t"}, "M-a": {"M-a"}, ".": {"."}}
	for in, want := range cases {
		got := splitLiteral(in)
		if len(got) != len(want) {
			t.Fatalf("splitLiteral(%q) = %q, want %q", in, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("splitLiteral(%q) = %q, want %q", in, got, want)
			}
		}
	}
}

// A mistyped chord (C-xyz, M-foo) stays ONE token so Press fails naming it,
// instead of quietly pressing "C", "-", "x", … as letters.
func TestMalformedChordIsAnError(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"C-xyz", "M-foo", "M-C-xyz"} {
		got := splitLiteral(bad)
		if len(got) != 1 || got[0] != bad {
			t.Errorf("splitLiteral(%q) = %q, want the one token", bad, got)
		}
		if _, err := keyMsgFor(bad); err == nil {
			t.Errorf("keyMsgFor(%q) must fail", bad)
		}
	}
}
