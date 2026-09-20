package markdown

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the testdata/*.json goldens")

// The goldens are the WIRE shape: internal/web's node test feeds these same
// files to static/markdown.js, so a change here is a change to what the
// painter must understand.
func TestGolden(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("testdata", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, md := range files {
		src, err := os.ReadFile(md)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", " ")
		if err := enc.Encode(Parse(string(src))); err != nil {
			t.Fatal(err)
		}
		golden := strings.TrimSuffix(md, ".md") + ".json"
		if *update {
			if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v (run go test -update)", err)
		}
		if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), buf.Bytes()) {
			t.Errorf("%s: tree differs from %s\n%s", md, golden, buf.String())
		}
	}
}

// The hostile fixture's markup must all have landed in text leaves.
func TestHostileFixtureIsInert(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "hostile.md"))
	if err != nil {
		t.Fatal(err)
	}
	d := Parse(string(src))
	checkDoc(t, d)
	if !strings.Contains(allText(d), "<script>window.__pwned=1</script>") {
		t.Error("the script tag must survive as literal text")
	}
}

// checkDoc asserts the invariants every consumer relies on.
func checkDoc(t *testing.T, d Doc) {
	t.Helper()
	var inline func([]Inline, int)
	inline = func(in []Inline, depth int) {
		if depth > MaxDepth+2 {
			t.Fatalf("inline nesting %d past the cap", depth)
		}
		for _, n := range in {
			if n.URL != "" && hasHTTPPrefix(n.URL) == 0 {
				t.Errorf("non-http URL in the tree: %q", n.URL)
			}
			if n.URL != "" && n.Kind != InLink && n.Kind != InImage {
				t.Errorf("a %q node carries a URL", n.Kind)
			}
			inline(n.In, depth+1)
		}
	}
	var blocks func([]Block, int)
	blocks = func(bs []Block, depth int) {
		if depth > MaxDepth+1 {
			t.Fatalf("block nesting %d past the cap", depth)
		}
		for _, b := range bs {
			if b.Lang != fenceLang(b.Lang) {
				t.Errorf("untamed lang %q", b.Lang)
			}
			for _, a := range b.Align {
				if a != "" && a != "left" && a != "center" && a != "right" {
					t.Errorf("align %q", a)
				}
			}
			inline(b.Inline, 0)
			for _, c := range b.Head {
				inline(c, 0)
			}
			for _, r := range b.Rows {
				if len(r) != len(b.Head) {
					t.Errorf("ragged table row: %d cells under %d heads", len(r), len(b.Head))
				}
				for _, c := range r {
					inline(c, 0)
				}
			}
			for _, l := range b.Lines {
				n := len([]rune(l.Text))
				for _, tk := range l.Toks {
					if tk.Start < 0 || tk.End > n || tk.Start >= tk.End {
						t.Errorf("token run [%d,%d) outside a %d-rune line", tk.Start, tk.End, n)
					}
				}
			}
			for _, it := range b.Items {
				blocks(it.Blocks, depth+1)
			}
			blocks(b.Blocks, depth+1)
		}
	}
	blocks(d.Blocks, 0)
}
