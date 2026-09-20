package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A pull request's diff names its sides for display, so its gg:// link is
// built from the pair the server hands out (refs/gg/pr/<n> against the base)
// — and refused when that pair is missing. Every link must reparse in Go.
func TestLinkForPullRequestJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "links.js"))
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), linksPureStart), strings.Index(string(src), linksPureEnd)
	if i < 0 || j < i {
		t.Fatal("links.js: the guarded section markers are gone")
	}
	dir := t.TempDir()
	pure := filepath.Join(dir, "pure.js")
	if err := os.WriteFile(pure, []byte(string(src)[i:j]), 0o644); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 40)
	script := `
import { readFileSync } from "node:fs";
const linkFor = new Function(readFileSync(process.argv[2], "utf8") + "; return linkFor;")();
const repo = { link_repo: "gigagit" };
const pr = (extra) => ({ path: "a.go", rev: "` + sha + `", state: "commit", compare: true,
  preview: { source: "alice:feat", target: "main", pr: 7, ...extra } });
console.log(JSON.stringify([
  linkFor(repo, "", pr({ linkSource: "refs/gg/pr/7", linkTarget: "main" }), "new", 3),
  linkFor(repo, "", pr({ linkSource: "refs/gg/pr/7", linkTarget: "` + sha + `" }), "old", 3),
  linkFor(repo, "", pr({}), "new", 3),
  linkFor(repo, "", pr({ linkSource: "refs/gg/pr/7", linkTarget: "" }), "new", 3),
]));
`
	run := filepath.Join(dir, "run.mjs")
	if err := os.WriteFile(run, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, run, pure).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	want := []string{
		"gg://gigagit/a.go@main...refs/gg/pr/7:3",
		"gg://gigagit/a.go@" + sha + "...refs/gg/pr/7", // a preview has no old side: the file form
		"", // no addressable pair: refuse rather than link the display names
		"",
	}
	for n := range want {
		if got[n] != want[n] {
			t.Errorf("link %d = %q, want %q", n, got[n], want[n])
		}
		if want[n] == "" {
			continue
		}
		if _, err := model.ParseLink(got[n]); err != nil {
			t.Errorf("%q does not reparse: %v", got[n], err)
		}
	}
}
