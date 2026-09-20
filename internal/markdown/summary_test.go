package markdown

import (
	"strings"
	"testing"
)

func TestSummary(t *testing.T) {
	t.Parallel()
	fence := "```"
	sugg := fence + "suggestion\nx\n" + fence
	cases := []struct{ name, src, sum, rest, raw string }{
		{"prose", "Fix **this**\n\nbecause", "Fix this", "because", "Fix **this**"},
		{"two lines", "line1\nline2\n\nmore", "line1", "line2\n\nmore", "line1"},
		{"heading", "## Title\nbody", "Title", "body", "Title"},
		{"list", "- a `b`\n- c", "a b", "- a `b`\n- c", "a `b`"},
		{"task list", "- [x] done\n- [ ] todo", "done", "- [x] done\n- [ ] todo", "done"},
		{"suggestion", sugg, LabelSuggestion, sugg, ""},
		{"code", fence + "go\nx\n" + fence, LabelCode, fence + "go\nx\n" + fence, ""},
		{"quote", "> said\n\nreply", LabelQuote, "> said\n\nreply", ""},
		{"table", "a|b\n-|-\n1|2", LabelTable, "a|b\n-|-\n1|2", ""},
		{"pipe prose", "a | b\nnot a table", "a | b", "not a table", "a | b"},
		{"rule", "---\nafter", LabelRule, "---\nafter", ""},
		{"empty", " \n ", "", "", ""},
		{"crlf", "one\r\ntwo", "one", "two", "one"},
		{"image only", "![](https://x/y.png)\nrest", "![](https://x/y.png)", "rest", "![](https://x/y.png)"},
		{"link", "see [the doc](https://d) now", "see the doc now", "", "see [the doc](https://d) now"},
	}
	for _, c := range cases {
		sum, rest, raw := Summary(c.src)
		if sum != c.sum || rest != c.rest || raw != c.raw {
			t.Errorf("%s: got (%q, %q, %q), want (%q, %q, %q)", c.name, sum, rest, raw, c.sum, c.rest, c.raw)
		}
	}
}

// Plain prose splits exactly as the first-line cut gg made before markdown.
func TestSummaryKeepsThePlainSplit(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"nice work",
		"Please rename this\nit shadows the package",
		"  padded first  \n\n  padded rest  \n",
		"one\n\n\ntwo\nthree",
		"tabs\tinside stay\nnext",
		"ends with newline\n",
	} {
		trimmed := strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
		first, rest, _ := strings.Cut(trimmed, "\n")
		sum, got, _ := Summary(body)
		if want := strings.Join(strings.Fields(first), " "); sum != want {
			t.Errorf("summary of %q = %q, want %q", body, sum, want)
		}
		if want := strings.TrimSpace(rest); got != want {
			t.Errorf("rest of %q = %q, want %q", body, got, want)
		}
	}
}

func TestPlainInline(t *testing.T) {
	t.Parallel()
	if got := PlainInline("see `x` and [t](https://u) **b** ![pic](https://i)  end"); got != "see x and t b pic end" {
		t.Errorf("got %q", got)
	}
}

func TestCodeColouring(t *testing.T) {
	t.Parallel()
	fence := "```"
	block := func(src string) Block { return Parse(src).Blocks[0] }
	has := func(l CodeLine, s, e int, c string) bool {
		for _, tk := range l.Toks {
			if tk.Start == s && tk.End == e && tk.Class == c {
				return true
			}
		}
		return false
	}
	b := block(fence + "go\nfunc main() {}\n" + fence)
	if !has(b.Lines[0], 0, 4, "kw") {
		t.Errorf("go: no kw run at [0,4): %+v", b.Lines[0].Toks)
	}
	// Offsets are RUNE offsets: the é before the keyword is one column.
	b = block(fence + "go\n/* é */ func x() {}\n" + fence)
	if !has(b.Lines[0], 8, 12, "kw") {
		t.Errorf("rune offsets: %+v", b.Lines[0].Toks)
	}
	for _, lang := range []string{"golang", "Go", "sh", "bash", "js", "ts", "py", "yml", "json"} {
		b = block(fence + lang + "\nx = \"s\" # 1\nif true; then echo 1; fi\n" + fence)
		n := 0
		for _, l := range b.Lines {
			n += len(l.Toks)
		}
		if n == 0 {
			t.Errorf("alias %q was not lexed", lang)
		}
	}
	for _, lang := range []string{"suggestion", "zzz", ""} {
		b = block(fence + lang + "\nfunc main() {}\n" + fence)
		if len(b.Lines[0].Toks) != 0 {
			t.Errorf("lang %q must stay plain", lang)
		}
	}
	long := fence + "go\n" + strings.Repeat("var x = 1\n", MaxLexLines+1) + fence
	if b = block(long); len(b.Lines) != MaxLexLines+1 || len(b.Lines[0].Toks) != 0 {
		t.Errorf("a block past MaxLexLines stays plain (%d lines)", len(b.Lines))
	}
	if b = block(fence + "\"><script>x</script>\ncode\n" + fence); b.Lang != "" {
		t.Errorf("a hostile info string must not become a language: %q", b.Lang)
	}
}
