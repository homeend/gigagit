package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkText(t *testing.T) {
	t.Parallel()
	dir := rangeRepo(t)
	_, out, _ := runLinkCLI(t, dir, "r.txt:3-5")
	link := strings.TrimSpace(out)

	code, out, errb := runLinkCLI(t, dir, "text", link)
	if want := "r.txt @ working tree (new), lines 3-5\n3\tl3\n4\tl4\n5\tl5\n"; code != 0 || out != want {
		t.Fatalf("exit %d, stdout %q (stderr %q), want %q", code, out, errb, want)
	}

	_, out, _ = runLinkCLI(t, dir, "--rev", "HEAD", "r.txt:2")
	code, out, _ = runLinkCLI(t, dir, "text", strings.TrimSpace(out))
	if code != 0 || !strings.Contains(out, "), line 2\n2\tl2\n") || strings.Contains(out, "working tree") {
		t.Errorf("a commit's single line: exit %d, %q", code, out)
	}

	code, out, _ = runLinkCLI(t, dir, "text", "--json", link)
	var w wireLinkText
	if err := json.Unmarshal([]byte(out), &w); err != nil || code != 0 || w.Path != "r.txt" || w.Target != "working tree" ||
		w.Side != "new" || w.Start != 3 || w.End != 5 || len(w.Lines) != 3 || w.Lines[2] != "l5" {
		t.Errorf("json: exit %d, %s (%v)", code, out, err)
	}

	// No line, or a hunk: nothing to print.
	_, out, _ = runLinkCLI(t, dir, "r.txt")
	if code, out, _ = runLinkCLI(t, dir, "text", strings.TrimSpace(out)); code != 2 || out != "" {
		t.Errorf("a file link: exit %d stdout %q, want 2 and nothing", code, out)
	}

	// The block changed: refused, no text.
	if err := os.WriteFile(filepath.Join(dir, "r.txt"), []byte("l1\nl2\nl3\nL4\nl5\nl6\nl7\nl8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb = runLinkCLI(t, dir, "text", link)
	if code != 1 || out != "" || !strings.Contains(errb, "the link is no longer valid: lines 3-5 of r.txt have changed since it was copied") {
		t.Errorf("stale: exit %d stdout %q stderr %q", code, out, errb)
	}
}
