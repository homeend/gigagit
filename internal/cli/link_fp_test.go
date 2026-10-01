package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// fpRepo: f.txt committed as alpha/beta/gamma, then line 2 edited (unstaged);
// g.txt has a blank second line.
func fpRepo(t *testing.T) string {
	t.Helper()
	dir := newCLIRepo(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("f.txt", "alpha\nbeta\ngamma\n")
	write("g.txt", "a\n\nb\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "f g")
	write("f.txt", "alpha\nbeta2\ngamma\n")
	return dir
}

func TestLinkFingerprintsAnUncommittedLine(t *testing.T) {
	t.Parallel()
	dir := fpRepo(t)
	fp := model.LineFingerprint
	for _, tc := range []struct {
		name string
		args []string
		want string // suffix of the printed link
	}{
		{"working file", []string{"f.txt:2"}, "/f.txt:2~" + fp("beta2")},
		{"staged = the index", []string{"--cached", "f.txt:1"}, "/f.txt@staged:1~" + fp("alpha")},
		{"old side = the index", []string{"f.txt:old:2"}, "/f.txt:old:2~" + fp("beta")},
		{"staged old side = HEAD", []string{"--cached", "f.txt:old:2"}, "/f.txt@staged:old:2~" + fp("beta")},
		{"content", []string{"--content", "f.txt:3"}, "/f.txt:3~" + fp("gamma") + "?view=content"},
		{"--no-fingerprint", []string{"--no-fingerprint", "f.txt:2"}, "/f.txt:2"},
		{"a commit link", []string{"--rev", "HEAD", "f.txt:2"}, ":2"},
		{"no line", []string{"f.txt"}, "/f.txt"},
		{"a blank line", []string{"g.txt:2"}, "/g.txt:2"},
		{"a line past the end", []string{"f.txt:99"}, "/f.txt:99"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out, errb := runLinkCLI(t, dir, tc.args...)
			if code != 0 {
				t.Fatalf("exit = %d (stderr %q)", code, errb)
			}
			got := strings.TrimSpace(out)
			if !strings.HasSuffix(got, tc.want) {
				t.Errorf("stdout = %q, want a link ending %q", got, tc.want)
			}
			if !strings.Contains(tc.want, "~") && strings.Contains(got, "~") {
				t.Errorf("stdout = %q, want no fingerprint", got)
			}
			if _, err := model.ParseLink(got); err != nil {
				t.Errorf("ParseLink(%q) = %v", got, err)
			}
		})
	}
}

func TestVerbsReportAMovedOrChangedLine(t *testing.T) {
	t.Parallel()
	dir := fpRepo(t)
	link := "gg://" + filepath.ToSlash(dir) + "/f.txt:2~" + model.LineFingerprint("beta2")

	// Still there: nothing is said.
	code, _, errb := runCLI(t, dir, "diff", link)
	if code != 0 || strings.Contains(errb, "gg: line") {
		t.Fatalf("same: exit=%d stderr=%q, want 0 and no note", code, errb)
	}

	// A line is inserted above: the text is on line 3 now.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("new\nalpha\nbeta2\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errb = runCLI(t, dir, "diff", link)
	if code != 0 || !strings.Contains(errb, "gg: line 2 moved to 3") {
		t.Fatalf("moved: exit=%d stderr=%q", code, errb)
	}
	code, out, errb := runCLI(t, dir, "link", "resolve", "--json", link)
	if code != 0 {
		t.Fatalf("resolve --json: exit=%d stderr=%q", code, errb)
	}
	var w struct {
		Line          int    `json:"line"`
		AskedLine     int    `json:"asked_line"`
		Anchor        string `json:"anchor"`
		AnchorMatches int    `json:"anchor_matches"`
	}
	if err := json.Unmarshal([]byte(out), &w); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if w.Line != 3 || w.AskedLine != 2 || w.Anchor != "moved" || w.AnchorMatches != 1 {
		t.Errorf("resolve --json = %+v", w)
	}
	code, out, _ = runCLI(t, dir, "link", "resolve", link)
	if code != 0 || !strings.Contains(out, "new:3 (line 2 moved to 3)") {
		t.Errorf("resolve = %d %q", code, out)
	}

	// The text itself is rewritten: said, never refused.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("new\nalpha\nzzz\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errb = runCLI(t, dir, "diff", link)
	if code != 0 || !strings.Contains(errb, "gg: line 2 has changed since this link was copied") {
		t.Fatalf("changed: exit=%d stderr=%q", code, errb)
	}

	// A plain link's JSON has none of the anchor fields.
	_, out, _ = runCLI(t, dir, "link", "resolve", "--json", "gg://"+filepath.ToSlash(dir)+"/f.txt:2")
	if strings.Contains(out, "anchor") || strings.Contains(out, "asked_line") {
		t.Errorf("plain link's JSON carries anchor fields: %s", out)
	}
}
