package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The CLI and the web write into the SAME per-repo link ring: `gg link`
// records through internal/cli.linkDesc, and the browser's "copy gg link"
// rows record through links.js's linkDesc twin, posted to /api/linkhist.
// Two spellings of one label would show the user two kinds of row for the
// same action, so the two implementations are pinned against each other here
// the way internal/web's TestLinkForJSMatchesGo pins the link producer.
//
// This test lives in internal/cli because internal/cli OWNS linkDesc (it is
// unexported, and the producers that call it are here); it reaches across to
// read the web package's static asset, which is only a file read.
const (
	linkDescJSRel = "../web/static/links.js"
	// The same guarded section markers TestLinkForJSMatchesGo uses — linkDesc
	// and truncateDesc live INSIDE it precisely so both gates cover them.
	linkDescPureStart = "// --- link producer (pure; guarded against Go) ---"
	linkDescPureEnd   = "// --- end link producer ---"
)

func TestLinkDescJSMatchesGo(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		// Same disposition as TestLinkForJSMatchesGo: a machine without node
		// loses this gate rather than the suite.
		t.Skip("node not installed; the JS port guard needs it")
	}
	src, err := os.ReadFile(linkDescJSRel)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), linkDescPureStart)
	j := strings.Index(string(src), linkDescPureEnd)
	if i < 0 || j < i {
		t.Fatalf("links.js: the guarded section markers are gone (%q / %q)", linkDescPureStart, linkDescPureEnd)
	}
	pure := string(src)[i:j]
	if !strings.Contains(pure, "function linkDesc(") {
		t.Fatal("links.js: linkDesc must live INSIDE the guarded section, or this gate reads nothing")
	}

	long := strings.Repeat("x", 80) // past descMax
	// "日" is a single UTF-16 code unit, so a plain JS .slice() cuts it the
	// same way [...s] does — a BMP character CANNOT tell the two apart, and a
	// case built on one silently proves nothing. "😀" (U+1F600) is a
	// surrogate PAIR: .slice(0, descMax) splits it and yields a lone
	// surrogate, while [...s] (and Go's []rune) keep whole code points.
	multi := strings.Repeat("日", 80)
	astral := strings.Repeat("😀", 80)
	boundary := strings.Repeat("y", 60)     // exactly descMax
	overBoundary := strings.Repeat("z", 61) // one past
	type tcase struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		ID      string `json:"id"`
		Subject string `json:"subject"`
	}
	cases := []tcase{
		{Name: "branch", Kind: "branch", ID: "feat/x"},
		{Name: "bookmark", Kind: "bookmark", ID: "my label"},
		{Name: "shelf", Kind: "shelf", ID: "wip bytes"},
		{Name: "preview", Kind: "preview", ID: "main...feat/x"},
		{Name: "file", Kind: "file", ID: "internal/web/files.js"},
		{Name: "commit", Kind: "commit", ID: "abc1234", Subject: "fix the thing"},
		{Name: "stash", Kind: "stash", Subject: "WIP on main"},
		// The fallback arm: a shape with no row in spec §4.3's table (a
		// --pair change-set, a --cached link, the bare working tree) records
		// as "link: <the link text>" rather than a false label.
		{Name: "fallback link", Kind: "link", ID: "gg://repo/a.txt@a..b"},
		// Truncation, on every arm that truncates.
		{Name: "long id truncates", Kind: "bookmark", ID: long},
		{Name: "long subject truncates", Kind: "commit", ID: "abc1234", Subject: long},
		{Name: "long stash subject truncates", Kind: "stash", Subject: long},
		{Name: "multibyte id cuts on runes", Kind: "bookmark", ID: multi},
		{Name: "multibyte subject cuts on runes", Kind: "commit", ID: "abc1234", Subject: multi},
		{Name: "astral id cuts on code points, not UTF-16 units", Kind: "bookmark", ID: astral},
		{Name: "astral subject cuts on code points", Kind: "commit", ID: "abc1234", Subject: astral},
		{Name: "exactly descMax is kept whole", Kind: "bookmark", ID: boundary},
		{Name: "one past descMax truncates", Kind: "bookmark", ID: overBoundary},
		// Whitespace is trimmed BEFORE the length check, so a padded label
		// under the cap must not be truncated by its own padding.
		{Name: "padded id is trimmed", Kind: "bookmark", ID: "   spaced   "},
		{Name: "padded subject is trimmed", Kind: "commit", ID: "abc1234", Subject: "  subject  "},
		{Name: "empty id", Kind: "bookmark", ID: ""},
		{Name: "empty subject", Kind: "commit", ID: "abc1234", Subject: ""},
	}

	want := make([]string, len(cases))
	for n, c := range cases {
		want[n] = linkDesc(c.Kind, c.ID, c.Subject)
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
const linkDesc = new Function(pure + "; return linkDesc;")();
console.log(JSON.stringify(cases.map((c) => linkDesc(c.kind, c.id, c.subject))));
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
		t.Fatalf("node returned %d descs, want %d", len(got), len(want))
	}
	for n := range want {
		if got[n] != want[n] {
			t.Errorf("case %q: links.js linkDesc = %q, cli.linkDesc = %q", cases[n].Name, got[n], want[n])
		}
	}
}
