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
  classList: { toggle() {} }, on: {}, addEventListener(type, fn) { this.on[type] = fn; }, querySelectorAll() { return []; }, querySelector() { return null; },
  scrollIntoView() {}, setPointerCapture() {}, getBoundingClientRect() { return { left: 0, width: 1 }; } });
globalThis.document = {
  title: "",
  getElementById(id) {
    if (id === "gg-data") return { textContent: DATA };
    return (els[id] ||= mk(id));
  },
  on: {},
  addEventListener(type, fn) { this.on[type] = fn; },
};
`

// runPageScript runs the rendered page's script on the DOM stub, then tail
// (JS reading els), and returns node's trimmed output.
func runPageScript(t *testing.T, tail string) string {
	t.Helper()
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
	src := "const DATA = " + jsString(data) + ";\n" + domStub + script + "\n" + tail + "\n"
	f := filepath.Join(t.TempDir(), "run.js")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, f).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestPageScriptWritesTheHeader(t *testing.T) {
	t.Parallel()
	got := runPageScript(t, "console.log(JSON.stringify({ meta: els['x-meta'].textContent, title: els['x-title'].textContent }));")
	want := `{"meta":"gigagit · 1a2b3c4 → 5d6e7f8 · exported 2026-10-12 10:00 by v0.0.0-test","title":"Why </script> broke"}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// n / p / D work while the file list holds focus (it does after a pick):
// the page handles them and stops them, so the list's type-to-find never
// jumps to a file starting with n; keys it does not own (arrows) pass.
func TestKeysWorkOnTheFileList(t *testing.T) {
	t.Parallel()
	got := runPageScript(t, `const key = (k) => { let stopped = false;
  document.on.keydown({ key: k, target: { tagName: "SELECT" }, preventDefault() { stopped = true; } }); return stopped; };
console.log(JSON.stringify({ n: key("n"), down: key("ArrowDown") }));`)
	if got != `{"n":true,"down":false}` {
		t.Fatalf("keys on the file list = %s", got)
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
