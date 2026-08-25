package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A commit message pasted from a web page (the test-1 "long description"
// commit) carries bare \r characters: \r\r between paragraphs, no \n at all
// inside the body. A raw \r tells the terminal to jump to column 0 mid-line
// and the rest of the row overwrites the popup's own border — invisible to
// every width calculation, so only a real terminal shows the corruption
// (the sanitizeForDisplay story, error_popup.go). These tests pin the three
// boundaries where such text must be neutralized.

// TestFileContentLinesNormalizesCR: the read-only content path (commit
// message view, file preview, finder preview). CRLF collapses to one line
// break, a lone \r BECOMES a line break (dropping it would glue words
// together), and no \r may survive into a rendered line.
func TestFileContentLinesNormalizesCR(t *testing.T) {
	t.Parallel()
	lines := fileContentLines([]byte("alpha\r\nbeta\rgamma\r\rdelta"))
	var texts []string
	for _, l := range lines {
		if strings.ContainsRune(l.text, '\r') {
			t.Fatalf("a \\r survived into a display line: %q", l.text)
		}
		texts = append(texts, l.text)
	}
	want := []string{"alpha", "beta", "gamma", "", "delta"}
	if len(texts) != len(want) {
		t.Fatalf("got %d lines %q, want %d %q", len(texts), texts, len(want), want)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, texts[i], want[i])
		}
	}
}

// TestNewTextFieldNormalizesCR: the edit-popup prefill path (reword / amend /
// irebase reword all pre-fill fields from an existing message). The buffer
// must never hold a \r rune — styledLines chunks would render it and corrupt
// the row — so CRLF and lone \r normalize to \n at construction.
func TestNewTextFieldNormalizesCR(t *testing.T) {
	t.Parallel()
	f := newTextField("one\r\ntwo\rthree")
	if got, want := f.Value(), "one\ntwo\nthree"; got != want {
		t.Fatalf("Value() = %q, want %q", got, want)
	}
	if f.cursor != len([]rune("one\ntwo\nthree")) {
		t.Fatalf("cursor = %d, want end of normalized buffer", f.cursor)
	}
}

// TestTextfieldInsertNormalizesCR: the paste path — bracketed paste delivers
// the raw runes, including CRLF from Windows clipboards and bare \r from web
// copies.
func TestTextfieldInsertNormalizesCR(t *testing.T) {
	t.Parallel()
	var f textfield
	f.insert([]rune("a\r\nb\rc"))
	if got, want := f.Value(), "a\nb\nc"; got != want {
		t.Fatalf("Value() = %q, want %q", got, want)
	}
}

// TestCommitMessagePopupOpensWrapped: a commit message is prose (the pasted
// body above is ONE multi-thousand-character logical line) — the view must
// open in wrap mode like the error popup does, not cutoff mode, or the body
// reads as a single truncated line.
func TestCommitMessagePopupOpensWrapped(t *testing.T) {
	t.Parallel()
	m := New(nil)
	m2, _ := m.openCommitMessagePopup(model.Commit{Hash: "0123456789abcdef0123456789abcdef01234567"})
	p := layerOf[*contentPopup](m2)
	if p == nil {
		t.Fatal("no contentPopup pushed")
	}
	if p.mode != modeWrap {
		t.Fatalf("commit message view mode = %v, want modeWrap", p.mode)
	}
}
