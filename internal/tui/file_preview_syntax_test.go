package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/syntax"
)

// legacyFileContentLines is fileContentLines exactly as it stood before the
// class mask was threaded through it — the oracle for "the plain path did not
// move a byte".
func legacyFileContentLines(data []byte) []contentLine {
	s := strings.TrimRight(string(data), "\n")
	if s == "" {
		return []contentLine{{text: i18n.T("(empty file)")}}
	}
	s = normalizeLineBreaks(s)
	parts := strings.Split(sanitizeForDisplay(s), "\n")
	out := make([]contentLine, len(parts))
	for i, ln := range parts {
		out[i] = contentLine{text: ln}
	}
	return out
}

// The fixture covers every normalization fileContentLines performs: tabs, C0
// controls (dropped, not substituted), CRLF, a bare \r, and trailing newlines.
const previewFixture = "package main\r\n\tif x {\x01}\r\rvar y\t= 1\n\n\n"

func TestFileContentLinesPlainPathUnchanged(t *testing.T) {
	t.Parallel()
	for _, data := range []string{previewFixture, "", "\n\n", "one line", "a\rb"} {
		want := legacyFileContentLines([]byte(data))
		got := fileContentLines([]byte(data))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("fileContentLines(%q) changed:\n got %#v\nwant %#v", data, got, want)
		}
		for _, l := range got {
			if l.cls != nil {
				t.Errorf("the plain path must attach no class mask: %#v", l)
			}
		}
	}
}

// The tokenised variant keeps the SAME text and adds a mask of exactly one
// class per display rune.
func TestFileContentLinesTokKeepsTextAndAlignsMask(t *testing.T) {
	t.Parallel()
	data := []byte("package main\n\tvar x = 1\n")
	tok := syntax.Lex("Go", data)
	if len(tok) < 2 {
		t.Fatalf("fixture should lex to 2 lines, got %d", len(tok))
	}
	plain := fileContentLines(data)
	got := fileContentLinesTok(data, tok)
	if len(got) != len(plain) {
		t.Fatalf("line count changed: %d vs %d", len(got), len(plain))
	}
	for i := range got {
		if got[i].text != plain[i].text {
			t.Errorf("line %d text changed: %q vs %q", i, got[i].text, plain[i].text)
		}
		if len(got[i].cls) != len([]rune(got[i].text)) {
			t.Errorf("line %d: mask has %d entries for %d display runes", i, len(got[i].cls), len([]rune(got[i].text)))
		}
	}
	if got[0].cls[0] != syntax.Keyword {
		t.Errorf("`package` should lex as a keyword, got %v", got[0].cls[0])
	}
	// The tab expanded to 4 spaces: `var` starts at display rune 4.
	if got[1].cls[4] != syntax.Keyword {
		t.Errorf("`var` after a tab should be a keyword at display rune 4, got %v", got[1].cls)
	}
	if got[1].cls[0] != syntax.Plain {
		t.Errorf("the expanded tab must stay plain, got %v", got[1].cls[0])
	}
}

// displayCls mirrors sanitizeForDisplay's per-rune map by hand, so the two are
// pinned together: one class per rune the sweep keeps, four for a tab, none for
// a dropped control — over content that exercises all three.
func TestFileContentLinesTokMaskLengthMatchesEveryLine(t *testing.T) {
	t.Parallel()
	data := []byte("\tif x {\x01}\r\nvar\ty\t= 1\n\n// tail\n")
	tok := syntax.Lex("Go", data)
	if tok == nil {
		t.Fatal("fixture should lex")
	}
	for i, l := range fileContentLinesTok(data, tok) {
		if len(l.cls) != len([]rune(l.text)) {
			t.Errorf("line %d (%q): mask has %d entries for %d display runes", i, l.text, len(l.cls), len([]rune(l.text)))
		}
	}
}

// previewOf opens the "View file" preview on path with the given file content.
func previewOf(t *testing.T, m Model, path, content string) Model {
	t.Helper()
	m = previewModelWith(m, content)
	m.filesView = &contentPopup{lines: []contentLine{{text: "M  " + path, path: path, status: "M"}}}
	m.filesView.sel = 0
	m.filesTreeFocused = true
	m.filesHash = "abc1234"
	row, ok := m.viewFileRow()
	if !ok {
		t.Fatal("View file should be offered")
	}
	updated, cmd := row.run(m)
	if cmd == nil {
		t.Fatal("View file should dispatch a content load")
	}
	next, _ := updated.(Model).Update(cmd())
	return next.(Model)
}

// previewModelWith rebuilds a preview-capable Model around base's config.
func previewModelWith(base Model, content string) Model {
	m := previewModelN(content)
	m.cfg = base.cfg
	return m
}

func TestPreviewLexesKnownLanguage(t *testing.T) {
	t.Parallel()
	m := previewOf(t, Model{}, "x.go", "package main\n\nfunc main() {}\n")
	if m.filesPreview == nil {
		t.Fatal("the preview should be open")
	}
	l := m.filesPreview.lines[0]
	if l.text != "package main" {
		t.Fatalf("first preview line = %q", l.text)
	}
	if len(l.cls) != len([]rune(l.text)) || l.cls[0] != syntax.Keyword {
		t.Errorf("the `package` line should carry a keyword mask: %v", l.cls)
	}
}

func TestPreviewSkipsUnknownLanguage(t *testing.T) {
	t.Parallel()
	m := previewOf(t, Model{}, "notes.txt", "package main\n")
	for i, l := range m.filesPreview.lines {
		if l.cls != nil {
			t.Errorf("line %d of a .txt preview must not be masked: %v", i, l.cls)
		}
	}
}

func TestPreviewSkipsWhenSyntaxOff(t *testing.T) {
	t.Parallel()
	base := Model{cfg: config.Config{UI: config.UIConfig{DiffSyntax: "off"}}}
	m := previewOf(t, base, "x.go", "package main\n")
	for i, l := range m.filesPreview.lines {
		if l.cls != nil {
			t.Errorf("line %d must not be masked with diff_syntax=off: %v", i, l.cls)
		}
	}
}

// A bare \r becomes a line break in the preview but stays inside its line for
// the lexer, so such a file is left plain rather than mis-coloured.
func TestPreviewSkipsBareCarriageReturn(t *testing.T) {
	t.Parallel()
	m := previewOf(t, Model{}, "x.go", "package main\rvar x = 1\n")
	for i, l := range m.filesPreview.lines {
		if l.cls != nil {
			t.Errorf("line %d must not be masked when the file holds a bare \\r: %v", i, l.cls)
		}
	}
	// CRLF is fine: both sides count the same lines.
	crlf := previewOf(t, Model{}, "x.go", "package main\r\nvar x = 1\r\n")
	if crlf.filesPreview.lines[0].cls == nil {
		t.Error("a CRLF file should still be coloured")
	}
}

// NOTE: serial (no t.Parallel) — lipgloss.SetColorProfile is process-global.
func TestFilePreviewRenderColoursCode(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	m := previewOf(t, Model{}, "x.go", "package main\n\nfunc main() {}\n")
	out := m.renderFilePreview(60, 12)
	if !strings.Contains(out, "38;5;"+syntaxColor(syntax.Keyword)) {
		t.Errorf("the preview should colour Go keywords:\n%q", out)
	}
	plain := previewOf(t, Model{}, "notes.txt", "package main\n").renderFilePreview(60, 12)
	if strings.Contains(plain, "38;5;"+syntaxColor(syntax.Keyword)) {
		t.Errorf("a .txt preview must stay uncoloured:\n%q", plain)
	}
}

// contentPopup.box offsets its rows by a two-column cursor/indent prefix; the
// mask must be offset with them, and the prefix itself stays plain.
func TestContentPopupBoxOffsetsClassMask(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	kw := make([]syntax.Class, len("package main"))
	for i := 0; i < 7; i++ {
		kw[i] = syntax.Keyword
	}
	p := newContentPopup("x.go", []contentLine{
		{text: "func main() {}"},
		{text: "package main", cls: kw},
	})
	out := p.box(Model{width: 100, height: 30})
	if !strings.Contains(out, "38;5;"+syntaxColor(syntax.Keyword)+"mpackage") {
		t.Errorf("the masked row should colour `package` past the two-column prefix:\n%q", out)
	}
	if !strings.Contains(out, "  \x1b["+"38;5;"+syntaxColor(syntax.Keyword)) {
		t.Errorf("the row prefix must stay plain before the coloured run:\n%q", out)
	}
}
