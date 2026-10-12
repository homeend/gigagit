package overviewhtml

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// domStub is just enough DOM for export.js to run under node: every element
// is a recorder; the test reads what the script wrote into #x-meta.
const domStub = `
const els = {};
const mk = (id) => ({ id, textContent: "", innerHTML: "", value: "", disabled: false, dataset: {}, style: { setProperty() {} },
  classList: { toggle() {} }, addEventListener() {}, querySelectorAll() { return []; }, querySelector() { return null; },
  scrollIntoView() {}, setPointerCapture() {}, getBoundingClientRect() { return { left: 0, width: 1 }; } });
globalThis.document = {
  title: "",
  getElementById(id) {
    if (id === "gg-data") return { textContent: DATA };
    return (els[id] ||= mk(id));
  },
  addEventListener() {},
};
`

func TestPageScriptWritesTheHeader(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	page, err := Render(fixture())
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	data := dataRE.FindStringSubmatch(s)[1]
	script := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindStringSubmatch(s)[1]
	src := "const DATA = " + jsString(data) + ";\n" + domStub + script +
		"\nconsole.log(JSON.stringify({ meta: els['x-meta'].textContent, title: els['x-title'].textContent }));\n"
	f := filepath.Join(t.TempDir(), "run.js")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, f).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `{"meta":"gigagit · 1a2b3c4 → 5d6e7f8 · exported 2026-10-12 10:00 by v0.0.0-test","title":"Why </script> broke"}`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// jsString quotes s as a JS string literal (JSON is a subset).
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
