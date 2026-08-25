package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// The file-list header's date line is rendered in the BROWSER: the server
// ships unix seconds because only the viewer knows the viewer's timezone, so
// files.js carries a port of the stamp the TUI writes with
// model.CommitDateLayout. Nothing but this test keeps the two in step, and the
// drift it prevents is the quiet kind — the same commit reading
// "2026-08-17 15:04" in the terminal and something else in the browser.
//
// Only the PURE section of files.js is evaluated: the rest of the module
// touches the DOM at import time and cannot run under node.
const metaPureStart = "// --- commit meta line (pure; guarded against Go) ---"
const metaPureEnd = "// --- end commit meta line ---"

func TestCommitMetaLineJSMatchesGo(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS port guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), metaPureStart)
	j := strings.Index(string(src), metaPureEnd)
	if i < 0 || j < i {
		t.Fatalf("files.js: the guarded section markers are gone (%q / %q)", metaPureStart, metaPureEnd)
	}
	pure := string(src)[i:j]

	// Cases worth pinning: a two-digit month/day/hour/minute, every field that
	// needs zero-padding, a year boundary, a commit with no author, and the
	// unknown date that must render as no line at all.
	type tcase struct {
		Time   int64  `json:"time"`
		Author string `json:"author"`
	}
	cases := []tcase{
		{981173106, "alice"},      // 2001-02-03 04:05:06Z — single digits everywhere
		{1786994089, "gigagit"},   // a recent commit
		{1767225599, "bob smith"}, // 2025-12-31 23:59:59Z — year boundary, spaced author
		{1000000000, ""},          // no author: the date stands alone
		{0, "alice"},              // unknown date: no line at all, author or not
	}

	want := make([]string, len(cases))
	for n, c := range cases {
		if c.Time == 0 {
			want[n] = "" // the Go side draws no line; see filesMetaLine in internal/tui
			continue
		}
		s := time.Unix(c.Time, 0).Format(model.CommitDateLayout)
		if c.Author != "" {
			s += " · " + c.Author
		}
		want[n] = s
	}

	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	blob, _ := json.Marshal(cases)
	if err := os.WriteFile(casesPath, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const cases = JSON.parse(readFileSync(process.argv[3], "utf8"));
const commitMetaLine = new Function(pure + "; return commitMetaLine;")();
console.log(JSON.stringify(cases.map(commitMetaLine)));
`
	purePath := filepath.Join(dir, "pure.js")
	scriptPath := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(purePath, []byte(pure), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, scriptPath, purePath, casesPath).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	if len(got) != len(want) {
		t.Fatalf("node returned %d lines, want %d", len(got), len(want))
	}
	for n := range want {
		if got[n] != want[n] {
			t.Errorf("case %d (time=%d author=%q): files.js renders %q, the TUI renders %q",
				n, cases[n].Time, cases[n].Author, got[n], want[n])
		}
	}
}
