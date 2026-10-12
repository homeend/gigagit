package overviewhtml

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/markdown"
)

func fixture() Export {
	doc := markdown.Parse("See [the loop](a.go:2-3).")
	return Export{
		Title: "Why </script> broke", Repo: "gigagit", Kind: "review", Left: "1a2b3c4", Right: "5d6e7f8",
		ExportedAt: "2026-10-12 10:00", Version: "v0.0.0-test", Blocks: doc.Blocks,
		Anchors: []Anchor{{Dest: "a.go:2-3", Path: "a.go", Start: 2, End: 3}},
		Files: []File{{Path: "a.go", Status: "M", Changed: true,
			Lines: []Line{{Text: "package a"}, {Text: `x := "</script><!--"`}, {Text: "y"}},
			Diff:  []DiffRow{{Kind: "same", Left: "package a", Right: "package a", LeftNo: 1, RightNo: 1}}}},
	}
}

var dataRE = regexp.MustCompile(`(?s)<script type="application/json" id="gg-data">(.*?)</script>`)

func TestRenderIsOneSelfContainedPage(t *testing.T) {
	t.Parallel()
	out, err := Render(fixture())
	if err != nil {
		t.Fatal(err)
	}
	page := string(out)
	for _, bad := range []string{"<script src", "<link", "fetch(", "XMLHttpRequest", "@import", "url(", "http://", "https://", "export function", "\nimport "} {
		if strings.Contains(page, bad) {
			t.Errorf("page contains %q", bad)
		}
	}
	if n := strings.Count(strings.ToLower(page), "</script>"); n != 2 { // the data blob + the one script
		t.Errorf("</script> count = %d, want 2 (file text must stay escaped)", n)
	}
	m := dataRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no data blob")
	}
	var back Export
	if err := json.Unmarshal([]byte(m[1]), &back); err != nil {
		t.Fatalf("blob: %v", err)
	}
	if back.Files[0].Lines[1].Text != `x := "</script><!--"` || back.Title != "Why </script> broke" {
		t.Errorf("round trip lost text: %+v", back.Files[0].Lines[1])
	}
	if !strings.Contains(page, "<title>Why &lt;/script&gt; broke</title>") {
		t.Error("title not escaped into <title>")
	}
}

func TestScopeModuleLeavesNoModuleSyntax(t *testing.T) {
	t.Parallel()
	got, err := scopeModule("MD", "export function a() {}\nexport const B = 1;\nfunction c() {}\nexport { c };\n", []string{"a", "B", "c"})
	if err != nil {
		t.Fatal(err)
	}
	want := "const MD = (() => {\nfunction a() {}\nconst B = 1;\nfunction c() {}\n\n\nreturn { a, B, c };\n})();\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	if _, err := scopeModule("X", "import { y } from './y.js';\n", nil); err == nil {
		t.Error("an import must be refused")
	}
}

func TestRenderRefusesTooLarge(t *testing.T) {
	t.Parallel()
	e := fixture()
	big := strings.Repeat("x", 1<<20)
	for i := range 20 {
		e.Files = append(e.Files, File{Path: fmt.Sprintf("d/%02d.txt", i), Lines: []Line{{Text: big}}})
	}
	_, err := Render(e)
	var tl *TooLargeError
	if !errors.As(err, &tl) || len(tl.Largest) != 3 || !strings.Contains(err.Error(), "16 MB cap") {
		t.Fatalf("err = %v", err)
	}
}

// The inlined script must at least parse: node --check on the extracted script.
func TestRenderScriptParses(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := Render(fixture())
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatal("no script")
	}
	f := filepath.Join(t.TempDir(), "page.js")
	if err := os.WriteFile(f, []byte(m[1]), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
		t.Fatalf("node --check: %v\n%s", err, b)
	}
}
