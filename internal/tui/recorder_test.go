package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyToken(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		tok  string
		ok   bool
	}{
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "enter", true},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "esc", true},
		{"space", tea.KeyMsg{Type: tea.KeySpace}, "space", true},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "tab", true},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "up", true},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, "down", true},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "left", true},
		{"right", tea.KeyMsg{Type: tea.KeyRight}, "right", true},
		{"bspace", tea.KeyMsg{Type: tea.KeyBackspace}, "bspace", true},
		{"dot", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")}, ".", true},
		{"letter", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}, "a", true},
		{"question", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}, "?", true},
		{"ctrl-g", tea.KeyMsg{Type: tea.KeyCtrlG}, "C-g", true},
		{"pgup-unsupported", tea.KeyMsg{Type: tea.KeyPgUp}, "", false},
		{"alt-down", tea.KeyMsg{Type: tea.KeyDown, Alt: true}, "", false},
		{"alt-left", tea.KeyMsg{Type: tea.KeyLeft, Alt: true}, "", false},
		{"alt-rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a"), Alt: true}, "", false},
		{"hash-literal", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("#")}, "#", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok, ok := keyToken(c.msg)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (tok %q)", ok, c.ok, tok)
			}
			if c.ok && tok != c.tok {
				t.Errorf("tok = %q, want %q", tok, c.tok)
			}
		})
	}
}

// The named tokens keyToken emits must be exactly the named keys
// tui-capture.sh's send_tokens recognizes (else replay mis-sends them as
// literal text). This is the round-trip contract, kept in lockstep by hand.
func TestKeyTokenNamedVocabulary(t *testing.T) {
	captureNamed := map[string]bool{
		"enter": true, "esc": true, "space": true, "tab": true,
		"up": true, "down": true, "left": true, "right": true, "bspace": true,
	}
	named := []tea.KeyMsg{
		{Type: tea.KeyEnter}, {Type: tea.KeyEsc}, {Type: tea.KeySpace},
		{Type: tea.KeyTab}, {Type: tea.KeyUp}, {Type: tea.KeyDown},
		{Type: tea.KeyLeft}, {Type: tea.KeyRight}, {Type: tea.KeyBackspace},
	}
	for _, m := range named {
		tok, ok := keyToken(m)
		if !ok || !captureNamed[tok] {
			t.Errorf("named key %v -> %q (ok %v): not in send_tokens' vocabulary", m.Type, tok, ok)
		}
	}
}

// nonCommentLines returns the non-empty, non-`#` lines of a recording.
func nonCommentLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		out = append(out, ln)
	}
	return out
}

func TestRecorderHeaderBodyAndDroppedQuit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.keys")
	r, err := newRecorder(path, "/some/repo")
	if err != nil {
		t.Fatal(err)
	}
	// . down enter q   (q is the terminating quit)
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	r.note(tea.KeyMsg{Type: tea.KeyDown})
	r.note(tea.KeyMsg{Type: tea.KeyEnter})
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	r.close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "# gg keystroke recording") || !strings.Contains(got, "# repo: /some/repo") {
		t.Errorf("header missing in:\n%s", got)
	}
	body := nonCommentLines(got)
	want := []string{".", "down", "enter"} // q dropped (terminating quit)
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
}

func TestRecorderCommentsUnsupportedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.keys")
	r, _ := newRecorder(path, "repo")
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	r.note(tea.KeyMsg{Type: tea.KeyPgUp}) // unsupported -> comment
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) // quit
	r.close()

	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "# unrecorded key:") {
		t.Errorf("expected an unrecorded-key comment, got:\n%s", s)
	}
	if body := nonCommentLines(s); !reflect.DeepEqual(body, []string{"a", "b"}) {
		t.Errorf("body = %v, want [a b]", body)
	}
}

func TestRecorderNilSafe(t *testing.T) {
	var r *recorder
	r.note(tea.KeyMsg{Type: tea.KeyEnter}) // must not panic
	r.close()                              // must not panic
}

// The Update tap must record a keystroke. Because recorder is a pointer field,
// both Update calls share it even though Update has a value receiver; the first
// key is flushed when the second arrives, and the second (buffered) is dropped
// at close — so exactly the first key lands in the file.
func TestUpdateRecordsKeystroke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.keys")
	r, err := newRecorder(path, "repo")
	if err != nil {
		t.Fatal(err)
	}
	m := New(nil)
	m.recorder = r
	m.Update(keyMsg("down")) // recorded (buffered)
	m.Update(keyMsg("down")) // flushes the first; buffers the second
	r.close()                // drops the buffered second

	got, _ := os.ReadFile(path)
	if body := nonCommentLines(string(got)); !reflect.DeepEqual(body, []string{"down"}) {
		t.Errorf("body = %v, want [down]", body)
	}
}
