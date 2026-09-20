package markdown

import (
	"strings"
	"testing"
	"time"
)

// inl renders an inline tree as an s-expression: text leaves are quoted.
func inl(in []Inline) string {
	var out []string
	for _, n := range in {
		switch n.Kind {
		case InText:
			out = append(out, `"`+n.Text+`"`)
		case InBreak:
			out = append(out, "br")
		case InCode:
			out = append(out, "code["+n.Text+"]")
		case InRef:
			out = append(out, "ref["+n.Text+"]")
		case InImage:
			out = append(out, "image["+n.URL+"]("+n.Text+")")
		case InLink:
			out = append(out, "link["+n.URL+"]("+inl(n.In)+")")
		default:
			out = append(out, n.Kind+"("+inl(n.In)+")")
		}
	}
	return strings.Join(out, " ")
}

func TestParseInline(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"emphasis", "**a** __b__ *c* _d_ ~~e~~", `strong("a") " " strong("b") " " em("c") " " em("d") " " del("e")`},
		{"snake case", "snake_case_name", `"snake_case_name"`},
		{"nested", "**a *b* c**", `strong("a " em("b") " c")`},
		{"em holds strong", "*a **b** c*", `em("a " strong("b") " c")`},
		{"triple", "***x***", `strong(em("x"))`},
		{"unclosed", "**a", `"**a"`},
		{"spaced star", "2 * 3 * 4", `"2 * 3 * 4"`},
		{"single tilde", "~x~", `del("x")`},
		{"code", "`x`", `code[x]`},
		{"code with tick", "``a`b``", "code[a`b]"},
		{"code is literal", "`**no**`", `code[**no**]`},
		{"code strips one space", "`` `x` ``", "code[`x`]"},
		{"unclosed code", "a ` b", "\"a ` b\""},
		{"link", `[t](https://e.x/p "title")`, `link[https://e.x/p]("t")`},
		{"link nests", "[**b**](http://x)", `link[http://x](strong("b"))`},
		{"link with parens", "[t](https://e.x/a_(b))", `link[https://e.x/a_(b)]("t")`},
		{"link bad space", "[t](<https://e.x/a b>)", `"t (https://e.x/a b)"`},
		{"empty link text", "[](https://e.x)", `link[https://e.x]("https://e.x")`},
		{"no dest", "[just brackets] here", `"[just brackets] here"`},
		{"reference style", "[a][b]", `"[a][b]"`},
		{"angle autolink", "<https://a.b>", `link[https://a.b]("https://a.b")`},
		{"bare url", "see https://a.b/c.", `"see " link[https://a.b/c]("https://a.b/c") "."`},
		{"bare url in parens", "(https://a.b)", `"(" link[https://a.b]("https://a.b") ")"`},
		{"bare url upper", "HTTPS://A.B", `link[HTTPS://A.B]("HTTPS://A.B")`},
		{"www is text", "www.x.y", `"www.x.y"`},
		{"not a url", "xhttps://a.b http:// h", `"xhttps://a.b http:// h"`},
		{"image", "![alt](https://i/x.png)", `image[https://i/x.png](alt)`},
		{"image no alt", "![](https://i/x.png)", `image[https://i/x.png]()`},
		{"bang alone", "wow! [x]", `"wow! [x]"`},
		{"js link", "[x](javascript:alert(1))", `"x"`},
		{"js link mixed case", "[x](JaVaScRiPt:1)", `"x"`},
		{"js link with tab", "[x](<java\tscript:1>)", `"x"`},
		{"data link", "[x](data:text/html,1)", `"x"`},
		{"vbscript link", "[x](vbscript:1)", `"x"`},
		{"js image", "![pic](javascript:1)", `"pic"`},
		{"relative link", "[x](../rel.md)", `"x (../rel.md)"`},
		{"mailto link", "[x](mailto:a@b)", `"x (mailto:a@b)"`},
		{"relative image", "![pic](img/a.png)", `"pic (img/a.png)"`},
		{"mention", "cc @octo-cat and @org/team.", `"cc " ref[@octo-cat] " and " ref[@org/team] "."`},
		{"email is text", "a@b.c", `"a@b.c"`},
		{"at alone", "@ home", `"@ home"`},
		{"issue", "fixes #123.", `"fixes " ref[#123] "."`},
		{"cross repo issue", "see org/repo#9", `"see " ref[org/repo#9]`},
		{"not an issue", "x#1y #a # c#1", `"x#1y #a # c#1"`},
		{"newline breaks", "a\nb", `"a" br "b"`},
		{"trailing spaces", "a  \nb", `"a" br "b"`},
		{"backslash break", "a\\\nb", `"a" br "b"`},
		{"escapes", `\*not\* \\ \a`, `"*not* \ \a"`},
		{"html", "<b>x</b> <!-- c -->", `"<b>x</b> <!-- c -->"`},
		{"html with handler", `<img src=x onerror=alert(1)>`, `"<img src=x onerror=alert(1)>"`},
		{"unicode", "**žluťoučký** kůň", `strong("žluťoučký") " kůň"`},
	}
	for _, c := range cases {
		if got := inl(ParseInline(c.src)); got != c.want {
			t.Errorf("%s:\n  got  %s\n  want %s", c.name, got, c.want)
		}
	}
}

func TestSafeURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want urlVerdict
	}{
		{"https://a.b/c?d=e#f", urlOK},
		{"http://a", urlOK},
		{"HTTP://A", urlOK},
		{"https://", urlShow},
		{"https://a b", urlShow},
		{"https://a\x01b", urlShow},
		{"//a.b", urlShow},
		{"ftp://a", urlShow},
		{"#anchor", urlShow},
		{"", urlDrop},
		{" javascript:x", urlDrop},
		{"java\nscript:x", urlDrop},
		{"DATA:x", urlDrop},
		{"https://" + strings.Repeat("a", maxLinkDest), urlShow},
	}
	for _, c := range cases {
		url, got := safeURL(c.in)
		if got != c.want {
			t.Errorf("safeURL(%q) = %v, want %v", c.in, got, c.want)
		}
		if (got == urlOK) != (url != "") {
			t.Errorf("safeURL(%q): a URL is returned exactly when the verdict is ok, got %q", c.in, url)
		}
	}
}

// Every Inline.URL a consumer can ever see is http(s).
func TestURLsAreAlwaysHTTP(t *testing.T) {
	t.Parallel()
	src := "[a](javascript:1) [b](https://ok) ![c](data:x) ![d](http://ok/i.png) <https://x> <javascript:1> [e](ftp://x)"
	var walk func([]Inline)
	walk = func(in []Inline) {
		for _, n := range in {
			if n.URL != "" && hasHTTPPrefix(n.URL) == 0 {
				t.Errorf("a non-http URL reached the tree: %q", n.URL)
			}
			walk(n.In)
		}
	}
	walk(ParseInline(src))
}

// Pathological runs of openers must not go quadratic.
func TestInlineIsBounded(t *testing.T) {
	t.Parallel()
	for _, unit := range []string{"*a ", "[", "`a ", "_a ", "![", "**x"} {
		src := strings.Repeat(unit, MaxInput/len(unit)-1)
		start := time.Now()
		ParseInline(src)
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("%q x many took %v", unit, d)
		}
	}
	deep := strings.Repeat("*_", 40) + "x" + strings.Repeat("_*", 40)
	if got := flatText(ParseInline(deep)); !strings.Contains(got, "x") {
		t.Errorf("deep nesting lost its text: %q", got)
	}
}
