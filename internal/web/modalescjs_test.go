package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The decision modal owns the keyboard while it is up, so esc MUST resolve
// to something or the user is trapped until they reach for the mouse. The
// engine's decisions carry a literal "abort"; the client-side confirms use
// plain words ("No", "Cancel", "stop"). escapeOption is the one rule that
// maps esc onto any of them — evaluated here under node from the pure
// section of ops.js (the rest of the module touches the DOM at import).
const modalPureStart = "// --- modal escape rule (pure; guarded) ---"
const modalPureEnd = "// --- end modal escape rule ---"

func TestModalEscapeOptionJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "ops.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), modalPureStart)
	j := strings.Index(string(src), modalPureEnd)
	if i < 0 || j < i {
		t.Fatalf("ops.js: the guarded section markers are gone (%q / %q)", modalPureStart, modalPureEnd)
	}
	pure := string(src)[i:j]

	cases := []struct {
		opts []string
		want string // "" = esc does nothing
	}{
		{[]string{"pull", "abort"}, "abort"},             // the engine rule wins
		{[]string{"No", "abort"}, "abort"},               // even when a softer word is present
		{[]string{"Yes", "No"}, "No"},                    // the fast-forward/reset confirms as shipped
		{[]string{"search deeper", "stop"}, "stop"},      // search.js
		{[]string{"go to worktree", "cancel"}, "cancel"}, // ops.js switch guard
		{[]string{"Push branch + tags", "Push branch only", "Cancel"}, "Cancel"},
		{[]string{"repair", "cancel"}, "cancel"},
		{[]string{"abort merge", "cancel"}, "cancel"}, // "abort merge" is an ACTION, not the escape
		{[]string{"discard", "keep"}, "keep"},
		{[]string{"run", "skip"}, "skip"}, // the post-create hook approval
		{[]string{"ours", "theirs"}, ""},  // no safe reading of esc — stays modal
		{[]string{}, ""},
	}
	script := pure + "\nconst cases = JSON.parse(process.argv[1]);\nconsole.log(JSON.stringify(cases.map((c) => escapeOption(c) ?? \"\")));\n"
	arg, _ := json.Marshal(func() [][]string {
		var all [][]string
		for _, c := range cases {
			all = append(all, c.opts)
		}
		return all
	}())
	cmd := exec.Command(node, "-e", script, "--", string(arg))
	cmd.Args = []string{node, "-e", script, string(arg)}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("bad node output %q: %v", out, err)
	}
	for n, c := range cases {
		if got[n] != c.want {
			t.Errorf("escapeOption(%q) = %q, want %q", c.opts, got[n], c.want)
		}
	}
}
