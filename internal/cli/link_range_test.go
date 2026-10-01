package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// rangeRepo: r.txt committed with eight lines, the index holds an extra first
// line, the working file a different one.
func rangeRepo(t *testing.T) string {
	t.Helper()
	dir := newCLIRepo(t)
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "r.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n")
	runGit(t, dir, "add", "r.txt")
	runGit(t, dir, "commit", "-m", "r")
	return dir
}

func TestLinkProducesARange(t *testing.T) {
	t.Parallel()
	dir := rangeRepo(t)
	fp := model.BlockFingerprint([]string{"l3", "l4", "l5"})
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"working file", []string{"r.txt:3-5"}, "/r.txt:3-5~" + fp},
		{"old side", []string{"r.txt:old:3-5"}, "/r.txt:old:3-5~" + fp},
		{"staged", []string{"--cached", "r.txt:3-5"}, "/r.txt@staged:3-5~" + fp},
		{"content", []string{"--content", "r.txt:3-5"}, "/r.txt:3-5~" + fp + "?view=content"},
		{"--no-fingerprint", []string{"--no-fingerprint", "r.txt:3-5"}, "/r.txt:3-5"},
		{"a commit", []string{"--rev", "HEAD", "r.txt:3-5"}, ":3-5"},
		{"one line", []string{"r.txt:3-3"}, "/r.txt:3~" + model.LineFingerprint("l3")},
	} {
		code, out, errb := runLinkCLI(t, dir, tc.args...)
		got := strings.TrimSpace(out)
		if code != 0 || !strings.HasSuffix(got, tc.want) {
			t.Errorf("%s: exit %d, stdout %q (stderr %q), want a link ending %q", tc.name, code, got, errb, tc.want)
		}
	}
	if code, _, _ := runLinkCLI(t, dir, "r.txt:5-3"); code != 2 {
		t.Errorf("a backwards range: exit %d, want 2", code)
	}
	if code, _, errb := runLinkCLI(t, dir, "--content", "r.txt:7-12"); code != 1 || !strings.Contains(errb, "8 lines") {
		t.Errorf("a content range past the end: exit %d (%q)", code, errb)
	}
}

func TestLinkResolveARange(t *testing.T) {
	t.Parallel()
	dir := rangeRepo(t)
	_, out, _ := runLinkCLI(t, dir, "r.txt:3-5")
	link := strings.TrimSpace(out)

	code, out, errb := runLinkCLI(t, dir, "resolve", "--json", link)
	if code != 0 {
		t.Fatalf("exit %d (%s)", code, errb)
	}
	var w struct {
		Line    int    `json:"line"`
		EndLine int    `json:"end_line"`
		Anchor  string `json:"anchor"`
	}
	if err := json.Unmarshal([]byte(out), &w); err != nil || w.Line != 3 || w.EndLine != 5 || w.Anchor != "same" {
		t.Fatalf("json = %s (%v)", out, err)
	}
	if _, out, _ = runLinkCLI(t, dir, "resolve", link); !strings.Contains(out, "new:3-5") {
		t.Errorf("plain output = %q, want new:3-5", out)
	}

	// The block changed: the link is refused, by every verb.
	if err := os.WriteFile(filepath.Join(dir, "r.txt"), []byte("l1\nl2\nl3\nL4\nl5\nl6\nl7\nl8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const stale = "the link is no longer valid: lines 3-5 of r.txt have changed since it was copied"
	if code, out, errb = runLinkCLI(t, dir, "resolve", link); code != 1 || out != "" || !strings.Contains(errb, stale) {
		t.Errorf("stale resolve: exit %d stdout %q stderr %q", code, out, errb)
	}
	if code, _, errb = runCLI(t, dir, "diff", link); code != 1 || !strings.Contains(errb, stale) {
		t.Errorf("stale diff: exit %d stderr %q", code, errb)
	}
}

func TestSessionHighlightAddTakesAFingerprintedRange(t *testing.T) {
	t.Parallel()
	inbox := t.TempDir()
	livePresence(t, inbox)
	repo := rangeRepo(t)
	svc := domain.Open(repo)
	var lout bytes.Buffer
	_ = runLink(linkState(t), svc, repo, []string{"r.txt:3-5"}, &lout, os.Stderr)
	link := strings.TrimSpace(lout.String())
	if !strings.Contains(link, "~") {
		t.Fatalf("link %q carries no fingerprint", link)
	}
	var out, errb bytes.Buffer
	if code := runSession(inbox, svc, []string{"highlight", "add", link, "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].Start != 3 || got[0].End != 5 {
		t.Fatalf("inbox = %+v, want a highlight 3-5", got)
	}
	errb.Reset()
	if code := runSession(inbox, svc, []string{"highlight", "add", link, "--end", "9", "--no-wait"}, &out, &errb); code != 2 {
		t.Errorf("--end beside a range link: exit %d (%q), want 2", code, errb.String())
	}
}

func TestNoteAddTakesARangeLink(t *testing.T) {
	dir := noteRepo(t) // a.txt: alpha / BRAVO / charlie / delta (line 2 unstaged)
	code, out, errb := runCLI(t, dir, "link", "a.txt:2-4")
	if code != 0 {
		t.Fatalf("link: %d (%s)", code, errb)
	}
	code, _, errb = runCLI(t, dir, "note", "add", strings.TrimSpace(out), "--summary", "these three")
	if code != 0 {
		t.Fatalf("note add: %d (%s)", code, errb)
	}
	_, out, _ = runCLI(t, dir, "note", "list", "--file", "a.txt", "--json")
	if !strings.Contains(out, `"range":[2,4]`) {
		t.Errorf("note list = %s, want range [2,4]", out)
	}
}
