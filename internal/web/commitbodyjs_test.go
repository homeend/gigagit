package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The file-list header draws the commit's DESCRIPTION — the message minus its
// subject — under the date line. files.js splits the two by git's own rule
// (the subject runs to the first blank line; the description is everything
// after it), and a wrong split shows the subject twice or eats the first
// paragraph. The section is pure, so node pins it.
func TestCommitBodyJSSplitsBySubject(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
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

	cases := []struct{ in, want string }{
		{"subject\n\nbody\n", "body"}, // the common shape
		{"subject only\n", ""},        // no description at all
		{"subject\n", ""},             // …nor after a bare newline
		{"", ""},                      // no message
		{"wrapped subject\ncontinues here\n\nbody\n", "body"},             // a multi-line subject is still the subject
		{"subject\n\n\nbody after two blanks\n", "body after two blanks"}, // extra blank lines do not become a leading line
		{"subject\n\npara one\n\npara two\n\n", "para one\n\npara two"},   // inner blank lines survive, trailing ones go
		{"subject\r\n\r\nwindows body\r\n", "windows body"},               // CRLF messages split too
	}
	ins := make([]string, len(cases))
	for n, c := range cases {
		ins[n] = c.in
	}
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	blob, _ := json.Marshal(ins)
	if err := os.WriteFile(casesPath, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const cases = JSON.parse(readFileSync(process.argv[3], "utf8"));
const commitBody = new Function(pure + "; return commitBody;")();
console.log(JSON.stringify(cases.map(commitBody)));
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
	if len(got) != len(cases) {
		t.Fatalf("node returned %d results, want %d", len(got), len(cases))
	}
	for n, c := range cases {
		if got[n] != c.want {
			t.Errorf("case %d (%q): commitBody = %q, want %q", n, c.in, got[n], c.want)
		}
	}
}
