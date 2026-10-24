package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hubFilter is the TUI content popup's visible(): a case-insensitive
// substring over the non-heading blocks; a heading stays only while a block
// under it matches; an empty query shows everything.
func TestHubFilterJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	mod := jsFunc(t, "prdetails.js", "hubFilter") + "\nexport { hubFilter };\n"
	harness := `
import { hubFilter } from "./h.mjs";
const items = [
  { heading: false, text: "#7 add the parser" },
  { heading: true, text: "description" },
  { heading: false, text: "Parses the TOML" },
  { heading: true, text: "conversation (1)" },
  { heading: false, text: "alice · 2d" },
  { heading: false, text: "looks fine" },
  { heading: true, text: "outdated review threads" },
  { heading: false, text: "parser.go" },
];
console.log(JSON.stringify([
  hubFilter(items, ""),
  hubFilter(items, "PARSE"),
  hubFilter(items, "fine"),
  hubFilter(items, "description"),
]));
`
	dir := t.TempDir()
	for name, body := range map[string]string{"h.mjs": mod, "run.mjs": harness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := "[[true,true,true,true,true,true,true,true]," +
		"[true,true,true,false,false,false,true,true]," +
		"[false,false,false,true,false,true,false,false]," +
		"[false,false,false,false,false,false,false,false]]"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("hubFilter =\n%s\nwant\n%s", got, want)
	}
}

// The details overlay answers the TUI hub's keys, each reaching the code the
// PR menu runs (no copies), and says so in its help.
func TestPRHubKeysWired(t *testing.T) {
	t.Parallel()
	det := readStatic(t, "prdetails.js")
	for _, want := range []string{
		`["y", "L", "r", "s", "v", "/"].includes(e.key)`, `e.key === "Enter"`,
		`key === "y"`, `key === "L"`, `key === "r"`, `key === "s"`, `key === "v"`, `key === "/"`,
		`import("./links.js")`, "copyPRLink(", // L: the menu's copy gg link
		`import("./prsendpanel.js")`, "openSendPanel(", // s
		`import("./prsend.js")`, "verdictBody(", // v
		`recallKey(e, input, "filetree"`, `recordSearch("filetree"`, "recallOpen(", // the content popup's ring
		"this pull request has no URL",
	} {
		if !strings.Contains(det, want) {
			t.Errorf("prdetails.js: missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "links.js"), "export async function copyPRLink(") {
		t.Error("links.js: copyPRLink is not exported")
	}
	if !strings.Contains(readStatic(t, "prsend.js"), "export function verdictBody(") {
		t.Error("prsend.js: verdictBody is not exported")
	}
	html := readStatic(t, "index.html")
	for _, id := range []string{`id="prdetails-keys"`, `id="prdetails-filter"`} {
		if !strings.Contains(html, id) {
			t.Errorf("index.html: no %s", id)
		}
	}
	for _, key := range []string{"<b>y</b>", "<b>L</b>", "<b>r</b>", "<b>s</b>", "<b>v</b>", "<b>/</b>"} {
		if !strings.Contains(det[strings.Index(det, "registerHelp("):], key) {
			t.Errorf("prdetails.js help: %s not named", key)
		}
	}
}
