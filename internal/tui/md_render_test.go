package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/syntax"
)

func mdText(rows []mdRow) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.text
	}
	return strings.Join(out, "\n")
}

// clsOf is the class every rune of sub wears in the first row holding it
// (-1: not found, or the run is not uniformly one class).
func clsOf(t *testing.T, rows []mdRow, sub string) syntax.Class {
	t.Helper()
	for _, r := range rows {
		i := strings.Index(r.text, sub)
		if i < 0 {
			continue
		}
		start := len([]rune(r.text[:i]))
		n := len([]rune(sub))
		c := r.cls[start]
		for _, x := range r.cls[start : start+n] {
			if x != c {
				t.Fatalf("%q is not one class: %v", sub, r.cls[start:start+n])
			}
		}
		return c
	}
	t.Fatalf("%q not in any row:\n%s", sub, mdText(rows))
	return 0
}

func checkMasks(t *testing.T, rows []mdRow, width int) {
	t.Helper()
	for _, r := range rows {
		if len(r.cls) != len([]rune(r.text)) {
			t.Fatalf("mask/text mismatch (%d vs %d) on %q", len(r.cls), len([]rune(r.text)), r.text)
		}
		if width > 0 && lipgloss.Width(r.text) > width {
			t.Fatalf("row wider than %d: %q", width, r.text)
		}
		if strings.ContainsAny(r.text, "\x1b\t\n\r") {
			t.Fatalf("control character reached a row: %q", r.text)
		}
	}
}

func TestMdRowsInline(t *testing.T) {
	t.Parallel()
	rows := mdRows(markdown.Parse("plain **bold** *it* ***both*** ~~gone~~ `code` [txt](https://u.x) https://bare.x @carol #12 ![pic](https://i.x/p.png)\nnext \x1b[31mred"), 0)
	checkMasks(t, rows, 0)
	if got, want := mdText(rows), "plain bold it both gone code txt (https://u.x) https://bare.x @carol #12 [image: pic] (https://i.x/p.png)\nnext ·[31mred"; got != want {
		t.Fatalf("text:\n got %q\nwant %q", got, want)
	}
	for sub, want := range map[string]syntax.Class{
		"plain": syntax.Plain, "bold": mdStrong, "it": mdEm, "both": mdStrongEm, "gone": mdDel, "code": mdCode,
		"txt": mdLink, "(https://u.x)": mdDim, "https://bare.x": mdLink, "@carol": mdRef, "#12": mdRef,
		"[image: pic]": mdLink,
	} {
		if got := clsOf(t, rows, sub); got != want {
			t.Errorf("%q wears class %d, want %d", sub, got, want)
		}
	}
}

func TestMdRowsBlocks(t *testing.T) {
	t.Parallel()
	fence := "```"
	src := "# Title\n\n- a\n- b\n  - c\n\n3. x\n4. y\n\n- [x] done\n- [ ] open\n\n> said **so**\n\n---\n\n" +
		fence + "go\nfunc main() {\n\treturn\n}\n" + fence + "\n\n" + fence + "suggestion\nx := 1\n" + fence
	rows := mdRows(markdown.Parse(src), 0)
	checkMasks(t, rows, 0)
	want := strings.Join([]string{
		"Title", "",
		"• a", "• b", "  • c", "",
		"3. x", "4. y", "",
		"☑ done", "☐ open", "",
		"│ said so", "",
		strings.Repeat("─", 20), "",
		"  func main() {", "      return", "  }", "",
		"suggestion", "  x := 1",
	}, "\n")
	if got := mdText(rows); got != want {
		t.Fatalf("layout:\n%s\n--- want ---\n%s", got, want)
	}
	if c := clsOf(t, rows, "Title"); c != mdHeading {
		t.Errorf("heading class %d", c)
	}
	if c := clsOf(t, rows, "func"); c != syntax.Keyword {
		t.Errorf("func wears %d, want the Keyword class", c)
	}
	// The tab before `return` was expanded: the keyword's run moved with it.
	if c := clsOf(t, rows, "return"); c != syntax.Keyword {
		t.Errorf("return (after a tab) wears %d, want Keyword", c)
	}
	if c := clsOf(t, rows, "│ "); c != mdQuote {
		t.Errorf("quote bar class %d", c)
	}
	if c := clsOf(t, rows, "so"); c != mdStrong {
		t.Errorf("strong inside a quote keeps its class, got %d", c)
	}
	if c := clsOf(t, rows, "suggestion"); c != mdDim {
		t.Errorf("caption class %d", c)
	}
}

func TestMdRowsWrapAndClip(t *testing.T) {
	t.Parallel()
	long := "- " + strings.Repeat("word ", 20) + "**end**\n\n> " + strings.Repeat("quoted ", 12) + "\n\n```\n" + strings.Repeat("x", 100) + "\n```\n\nsupercalifragilisticexpialidocious-and-then-some-more-letters"
	rows := mdRows(markdown.Parse(long), 30)
	checkMasks(t, rows, 30)
	text := mdText(rows)
	if n := strings.Count(text, "word"); n != 20 {
		t.Errorf("wrapping lost words: %d of 20", n)
	}
	var item, quote, code []string
	for _, r := range rows {
		switch {
		case strings.HasPrefix(r.text, "• ") || strings.HasPrefix(r.text, "  w") || strings.HasPrefix(r.text, "  e"):
			item = append(item, r.text)
		case strings.HasPrefix(r.text, "│ "):
			quote = append(quote, r.text)
		case strings.HasPrefix(r.text, "  x"):
			code = append(code, r.text)
		}
	}
	if len(item) < 4 || !strings.HasPrefix(item[1], "  word") {
		t.Errorf("a wrapped item hangs under its text: %q", item)
	}
	if len(quote) < 3 {
		t.Errorf("every wrapped quote row keeps its bar: %q", quote)
	}
	if len(code) != 1 || lipgloss.Width(code[0]) != 30 {
		t.Errorf("code is clipped, never wrapped: %q", code)
	}
	if c := clsOf(t, rows, "end"); c != mdStrong {
		t.Errorf("a class survives the wrap, got %d", c)
	}
	if !strings.Contains(strings.ReplaceAll(text, "\n", ""), "supercalifragilisticexpialidocious-and-then-some-more-letters") {
		t.Errorf("an over-long word is hard-split, not lost:\n%s", text)
	}
}

func TestMdRowsTable(t *testing.T) {
	t.Parallel()
	rows := mdRows(markdown.Parse("| Name | Count | 名 |\n|:--|--:|:-:|\n| alpha | 1 | 日本 |\n| b | 1000 | x |"), 0)
	checkMasks(t, rows, 0)
	want := "Name  │ Count │  名 \n──────┼───────┼─────\nalpha │     1 │ 日本\nb     │  1000 │  x  "
	if got := mdText(rows); got != want {
		t.Fatalf("table:\n%s\n--- want ---\n%s", got, want)
	}
	if c := clsOf(t, rows, "Name"); c != mdStrong {
		t.Errorf("header class %d", c)
	}
	w := lipgloss.Width(rows[0].text)
	for _, r := range rows {
		if lipgloss.Width(r.text) != w {
			t.Errorf("ragged table row %q (%d vs %d)", r.text, lipgloss.Width(r.text), w)
		}
	}
	clipped := mdRows(markdown.Parse("| Name | Count |\n|--|--|\n| alpha | 1 |"), 8)
	checkMasks(t, clipped, 8)
}

// Every fixture the parser is pinned to lays out with sound masks at any width.
func TestMdRowsOverTheParserFixtures(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("..", "markdown", "testdata", "*.md"))
	if err != nil || len(files) < 6 {
		t.Fatalf("fixtures: %v %v", files, err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc := markdown.Parse(string(src))
		for _, w := range []int{0, 1, 7, 24, 50, 200} {
			checkMasks(t, mdRows(doc, w), w)
		}
	}
	if rows := mdRows(markdown.Doc{}, 40); len(rows) != 0 {
		t.Errorf("an empty doc lays out no rows: %v", rows)
	}
}

func TestMdInlineRowsLead(t *testing.T) {
	t.Parallel()
	rows := mdInlineRows(markdown.ParseInline("Rename `x` to **y** "+strings.Repeat("and more ", 6)), 24, "↳ carol: ")
	checkMasks(t, rows, 24)
	if !strings.HasPrefix(rows[0].text, "↳ carol: Rename x") || len(rows) < 3 {
		t.Fatalf("rows = %q", mdText(rows))
	}
	if c := clsOf(t, rows, "↳ carol: "); c != syntax.Plain {
		t.Errorf("the lead is plain, got %d", c)
	}
	if c := clsOf(t, rows, "x"); c != mdCode {
		t.Errorf("inline code class %d", c)
	}
}

func TestSyntaxStyleMarkdownClasses(t *testing.T) {
	t.Parallel()
	s := st()
	base := lipgloss.NewStyle()
	if !s.syntaxStyle(base, mdStrong).GetBold() || !s.syntaxStyle(base, mdStrongEm).GetItalic() ||
		!s.syntaxStyle(base, mdEm).GetItalic() || !s.syntaxStyle(base, mdDel).GetStrikethrough() ||
		!s.syntaxStyle(base, mdLink).GetUnderline() || !s.syntaxStyle(base, mdDim).GetFaint() ||
		!s.syntaxStyle(base, mdHeading).GetBold() || !s.syntaxStyle(base, mdQuote).GetFaint() || !s.syntaxStyle(base, mdRef).GetBold() {
		t.Error("a markdown class lost its attribute")
	}
	code := s.syntaxStyle(base, mdCode)
	if col := s.syntaxColor(syntax.String); col != "" {
		if code.GetForeground() != lipgloss.Color(col) {
			t.Error("inline code takes the String colour")
		}
	} else if !code.GetBold() {
		t.Error("with no palette, inline code is bold")
	}
	if got, want := s.syntaxStyle(base, syntax.Keyword).Render("x"), s.syntaxStyle(base, syntax.Keyword).Render("x"); got != want {
		t.Error("syntax classes are unchanged")
	}
	if s.syntaxStyle(base, syntax.Class(250)).Render("x") != base.Render("x") {
		t.Error("an unknown class renders as the base")
	}
}
