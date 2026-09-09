package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/syntax"
)

// blamePorcelain renders a minimal `git blame --porcelain` body for content,
// all lines attributed to one commit (see git.ParseBlamePorcelain).
func blamePorcelain(content ...string) string {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var b strings.Builder
	for i, ln := range content {
		fmt.Fprintf(&b, "%s %d %d 1\n", sha, i+1, i+1)
		if i == 0 {
			b.WriteString("author Ada\nauthor-time 1000000000\nsummary one\n")
		}
		b.WriteString("\t" + ln + "\n")
	}
	return b.String()
}

// blameLoaderModel is a Model whose svc answers `git blame` with content.
func blameLoaderModel(content ...string) Model {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git blame", gitexec.Result{Stdout: blamePorcelain(content...)})
	return Model{svc: domain.New(&git.Repo{Runner: f}), width: 100, height: 30}
}

func loadBlame(t *testing.T, m Model, path string) blameMsg {
	t.Helper()
	ctx := navContext{path: path}
	msg, ok := m.loadBlameCmd(ctx, "tag")().(blameMsg)
	if !ok {
		t.Fatalf("loadBlameCmd did not produce a blameMsg")
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	return msg
}

func TestBlameLoaderLexesKnownLanguage(t *testing.T) {
	t.Parallel()
	msg := loadBlame(t, blameLoaderModel("package main", "", "func main() {}"), "a.go")
	if len(msg.lines) != 3 {
		t.Fatalf("want 3 blame lines, got %d", len(msg.lines))
	}
	if len(msg.tok) != 3 {
		t.Fatalf("want one token slice per line, got %d", len(msg.tok))
	}
	if len(tokAt(msg.tok, 1)) == 0 {
		t.Errorf("the `package` line should carry syntax runs: %+v", msg.tok)
	}
	if tokAt(msg.tok, 1)[0].Class != syntax.Keyword {
		t.Errorf("`package` should lex as a keyword, got %+v", msg.tok[0])
	}
	// Line 3 is the one after a blank line: numbering must not drift.
	if len(tokAt(msg.tok, 3)) == 0 {
		t.Errorf("line 3 (`func main() {}`) should carry runs: %+v", msg.tok)
	}
}

func TestBlameLoaderSkipsUnknownLanguage(t *testing.T) {
	t.Parallel()
	if msg := loadBlame(t, blameLoaderModel("package main"), "a.unknownext"); msg.tok != nil {
		t.Errorf("a file with no lexer must not be lexed: %+v", msg.tok)
	}
}

func TestBlameLoaderSkipsWhenSyntaxOff(t *testing.T) {
	t.Parallel()
	m := blameLoaderModel("package main")
	m.cfg = config.Config{UI: config.UIConfig{DiffSyntax: "off"}}
	if msg := loadBlame(t, m, "a.go"); msg.tok != nil {
		t.Errorf("diff_syntax=off must disable blame colouring: %+v", msg.tok)
	}
}

// A bare \r (not part of a CRLF) makes the preview/lexer line numbering
// disagree, so such a file is left plain. CRLF content stays coloured.
func TestBlameLoaderSkipsBareCarriageReturn(t *testing.T) {
	t.Parallel()
	if msg := loadBlame(t, blameLoaderModel("package main\rvar x = 1"), "a.go"); msg.tok != nil {
		t.Errorf("a bare \\r must disable colouring: %+v", msg.tok)
	}
	if msg := loadBlame(t, blameLoaderModel("package main\r", "var x = 1\r"), "a.go"); msg.tok == nil {
		t.Error("CRLF content should still be coloured")
	}
}

func TestBlameMsgCarriesTokensIntoTheView(t *testing.T) {
	t.Parallel()
	b := newBlameView(navContext{path: "a.go"})
	m := Model{width: 100, height: 30}
	m = m.pushLayer(b)
	tok := [][]syntax.Tok{{{Start: 0, End: 7, Class: syntax.Keyword}}}
	updated, _ := m.Update(blameMsg{tag: b.tag, lines: blameFixture().lines, tok: tok})
	got := updated.(Model).topLayer().(*blameView)
	if len(got.tok) != 1 {
		t.Fatalf("the blame view should keep the loader's tokens, got %+v", got.tok)
	}
}

// NOTE: serial (no t.Parallel) — lipgloss.SetColorProfile is process-global.
func TestBlameRenderColoursCodeAndKeepsCursor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	b := blameFixture() // package main / func main() {} / dirty
	b.tok = [][]syntax.Tok{
		{{Start: 0, End: 7, Class: syntax.Keyword}},
		{{Start: 0, End: 4, Class: syntax.Keyword}},
		nil,
	}
	out := b.render(Model{width: 100, height: 30}, "")
	if !strings.Contains(out, "38;5;"+st().syntaxColor(syntax.Keyword)) {
		t.Errorf("blame code should carry syntax colours:\n%q", out)
	}
	if !strings.Contains(out, "\x1b[7m") {
		t.Errorf("the selected line must keep its reverse-video highlight:\n%q", out)
	}
	// The gutter is never coloured: no escape may open before the author name.
	for _, ln := range strings.Split(out, "\n") {
		if i := strings.Index(ln, "Ada"); i >= 0 && strings.Contains(ln[:i], "38;5;") {
			t.Errorf("the gutter must stay plain: %q", ln)
		}
	}
}

// Without tokens the blame render is byte-identical to the pre-syntax path.
func TestBlameRenderWithoutTokensUnchanged(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	m := Model{width: 100, height: 30}
	plain := blameFixture().render(m, "")
	if strings.Contains(plain, "38;5;") {
		t.Errorf("an untokenised blame must carry no syntax colour:\n%q", plain)
	}
}
