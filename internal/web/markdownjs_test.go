package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// markdown.js is a PAINTER: it turns the tree internal/markdown parsed into
// HTML and never parses markdown itself. It is import-free so it runs under
// node, against the very goldens the Go parser is pinned to — and against
// hand-built hostile trees, because the page must stay safe even if the tree
// it is sent is not the one the parser would have made.
func TestMarkdownJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "markdown.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "markdown.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	goldens, err := filepath.Glob(filepath.Join("..", "markdown", "testdata", "*.json"))
	if err != nil || len(goldens) < 6 {
		t.Fatalf("goldens: %v %v", goldens, err)
	}
	trees := map[string]json.RawMessage{}
	for _, g := range goldens {
		b, err := os.ReadFile(g)
		if err != nil {
			t.Fatal(err)
		}
		trees[strings.TrimSuffix(filepath.Base(g), ".json")] = b
	}
	treesJSON, _ := json.Marshal(trees)
	if err := os.WriteFile(filepath.Join(dir, "trees.json"), treesJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { readFileSync } from "node:fs";
import { mdHTML, mdInlineHTML } from "./markdown.mjs";
const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const trees = JSON.parse(readFileSync(new URL("./trees.json", import.meta.url)));
const html = {};
for (const [k, d] of Object.entries(trees)) html[k] = mdHTML(d, esc);
const evil = { blocks: [
  { k: "p", in: [
    { k: "link", url: "javascript:alert(1)", in: [{ k: "text", t: "jslink" }] },
    { k: "link", url: " https://spaced", in: [{ k: "text", t: "spaced" }] },
    { k: "image", url: "data:text/html,x", t: "dataimg" },
    { k: "script", t: "<script>x</script>", in: [{ k: "text", t: "kid" }] },
    { k: "link", url: 'https://ok/"onmouseover="x', in: [{ k: "text", t: "quoted" }] },
  ] },
  { k: "code", lang: '"><script>x</script>', lines: [{ t: "abc", toks: [
    { s: 0, e: 1, c: 'x" onload="y' }, { s: 1, e: 99, c: "kw" }, { s: -4, e: 2, c: "kw" }, { s: 1, e: 2, c: "kw" } ] }] },
  { k: "table", align: ['x" onload="y', "right"], head: [[{ k: "text", t: "h1" }], [{ k: "text", t: "h2" }]], rows: [[[{ k: "text", t: "c" }]]] },
  { k: "list", ordered: true, start: '1" onload="y', items: [{ task: "<b>", blocks: [{ k: "p", in: [{ k: "text", t: "item" }] }] }] },
  { k: "h", level: 99, in: [{ k: "text", t: "deep" }] },
  { k: "iframe", in: [{ k: "text", t: "unknown block" }] },
  null, 7, "str",
] };
let deep = { k: "quote", blocks: [] };
for (let i = 0, cur = deep; i < 200; i++) { const n = { k: "quote", blocks: [{ k: "p", in: [{ k: "text", t: "d" + i }] }] }; cur.blocks.push(n); cur = n; }
console.log(JSON.stringify({
  html,
  evil: mdHTML(evil, esc),
  deep: mdHTML({ blocks: [deep] }, esc).length > 0,
  empty: [mdHTML(null, esc), mdHTML(undefined, esc), mdHTML({}, esc), mdHTML({ blocks: "x" }, esc), mdInlineHTML(null, esc), mdInlineHTML("x", esc)],
  capOff: mdHTML(trees.thread, esc, { skipFirstCaption: true }),
  capFirst: mdHTML({ blocks: [{ k: "code", lang: "suggestion", lines: [{ t: "a" }] }, { k: "code", lang: "suggestion", lines: [{ t: "b" }] }] }, esc, { skipFirstCaption: true }),
  inline: mdInlineHTML([{ k: "text", t: "Rename " }, { k: "code", t: "<x>" }, { k: "strong", in: [{ k: "text", t: "now" }] }], esc),
}));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		HTML             map[string]string
		Evil             string
		Deep             bool
		Empty            []string
		Inline           string
		CapOff, CapFirst string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	wants := map[string][]string{
		"basic": {
			`<h3 class="md-h md-h1">Forge tab</h3>`, `<h4 class="md-h md-h2">Details</h4>`,
			`<strong>forge</strong>`, `<em>read-only</em>`, `<del>no writes</del>`, `<code class="md-ic">inline code</code>`,
			`<a href="https://example.com/spec" target="_blank" rel="noopener noreferrer" title="https://example.com/spec">the spec</a>`,
			`>https://example.com/plain</a>`, `<span class="md-ref">@carol</span>`, `<span class="md-ref">octo/repo#7</span>`,
			`title="https://example.com/shot.png">[image: screenshot]</a>`, `<hr class="md-hr">`,
			`<blockquote class="md-q"><p>quoted <strong>reply</strong></p><blockquote class="md-q">`, `<br>`,
		},
		"lists": {
			`<ul class="md-list">`, `<ol class="md-list" start="3">`, `<li class="md-task"><p><span class="md-box">☐</span> open task</p></li>`,
			`<span class="md-box">☑</span> done task`, `<li><p>second with <code class="md-ic">code</code></p><ul class="md-list">`,
		},
		"code": {
			`<pre class="md-code" data-lang="go"><code>`, `<span class="tk-kw">func</span>`, `<span class="tk-cmt">// say hi</span>`,
			`<div class="md-cap">suggestion</div><pre class="md-code" data-lang="suggestion"><code>x := compute(y)</code></pre>`,
			`plain &lt;b&gt;text&lt;/b&gt;`,
		},
		"table": {
			`<table class="md-table">`, `<th class="md-al-left">Name</th>`, `<th class="md-al-right">Count</th>`, `<th class="md-al-center">Note</th>`,
			`<td class="md-al-center"><strong>bold</strong></td>`, `<td class="md-al-left">beta | gamma</td>`, `<td class="md-al-center"></td>`,
		},
		"thread": {`<ul class="md-list">`, `<div class="md-cap">suggestion</div>`},
	}
	for name, subs := range wants {
		for _, sub := range subs {
			if !strings.Contains(got.HTML[name], sub) {
				t.Errorf("%s: missing %s\n%s", name, sub, got.HTML[name])
			}
		}
	}

	all := map[string]string{"evil": got.Evil}
	for k, v := range got.HTML {
		all[k] = v
	}
	for name, h := range all {
		assertClosedHTML(t, name, h)
		for _, bad := range []string{"<script", "<img", "<iframe"} {
			if strings.Contains(h, bad) {
				t.Errorf("%s: %q reached the HTML\n%s", name, bad, h)
			}
		}
	}
	if strings.Contains(got.Evil, "onload") || strings.Contains(got.Evil, "onmouseover=\"") {
		t.Errorf("evil: an injected attribute survived\n%s", got.Evil)
	}
	if !strings.Contains(got.HTML["hostile"], "&lt;script&gt;window.__pwned=1&lt;/script&gt;") {
		t.Errorf("hostile: the script must show as text\n%s", got.HTML["hostile"])
	}
	for _, sub := range []string{"jslink", "spaced", "[image: dataimg]", "quoted", "kid", "unknown block", `<pre class="md-code"><code>`,
		`<span class="tk-kw">b</span>`, `<th>h1</th>`, `<th class="md-al-right">h2</th>`, `<ol class="md-list">`, `<li><p>item</p></li>`, `md-h6`} {
		if !strings.Contains(got.Evil, sub) {
			t.Errorf("evil: missing %s\n%s", sub, got.Evil)
		}
	}
	if n := strings.Count(got.Evil, "<a "); n != 1 {
		t.Errorf("evil: %d links painted, want only the https one\n%s", n, got.Evil)
	}
	// skipFirstCaption drops only a LEADING suggestion's caption: the thread
	// fixture opens with prose, so its (later) suggestion keeps the caption.
	if strings.Count(got.CapOff, "md-cap") != 1 || strings.Count(got.CapFirst, "md-cap") != 1 || !strings.HasPrefix(got.CapFirst, "<pre") {
		t.Errorf("skipFirstCaption: thread=%d first=%q", strings.Count(got.CapOff, "md-cap"), got.CapFirst)
	}
	if !got.Deep {
		t.Error("a 200-deep tree must paint (bounded), not throw")
	}
	for i, e := range got.Empty {
		if e != "" {
			t.Errorf("empty[%d] = %q", i, e)
		}
	}
	if want := `Rename <code class="md-ic">&lt;x&gt;</code><strong>now</strong>`; got.Inline != want {
		t.Errorf("inline = %s", got.Inline)
	}
	if strings.Contains(string(src), "import ") || strings.Contains(string(src), "innerHTML") || strings.Contains(string(src), "document") {
		t.Error("markdown.js stays import-free and DOM-free: it returns strings")
	}
}

// assertClosedHTML: every tag is from the painter's closed list and carries
// only its known attributes.
func assertClosedHTML(t *testing.T, name, h string) {
	t.Helper()
	tags := map[string]bool{"p": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true, "li": true,
		"blockquote": true, "pre": true, "code": true, "table": true, "thead": true, "tbody": true, "tr": true, "th": true,
		"td": true, "hr": true, "strong": true, "em": true, "del": true, "a": true, "span": true, "br": true, "div": true}
	attrs := map[string]bool{"class": true, "href": true, "target": true, "rel": true, "title": true, "start": true, "data-lang": true}
	for i := 0; i < len(h); i++ {
		if h[i] != '<' {
			continue
		}
		end := strings.IndexByte(h[i:], '>')
		if end < 0 {
			t.Fatalf("%s: unclosed tag at %d", name, i)
		}
		body := strings.TrimPrefix(h[i+1:i+end], "/")
		tag, rest, _ := strings.Cut(body, " ")
		if !tags[tag] {
			t.Errorf("%s: tag <%s> is not in the closed list", name, tag)
		}
		for rest != "" {
			key, after, ok := strings.Cut(rest, `="`)
			if !ok {
				t.Errorf("%s: malformed attributes %q", name, rest)
				break
			}
			if !attrs[strings.TrimSpace(key)] {
				t.Errorf("%s: attribute %q is not allowed", name, key)
			}
			var val string
			val, rest, _ = strings.Cut(after, `"`)
			if strings.TrimSpace(key) == "href" && !strings.HasPrefix(val, "https://") && !strings.HasPrefix(val, "http://") {
				t.Errorf("%s: href %q is not http(s)", name, val)
			}
			rest = strings.TrimSpace(rest)
		}
		i += end
	}
}
