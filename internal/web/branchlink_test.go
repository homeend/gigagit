package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A branch's gg link is its tip by name, the TUI's refLinkFor: no path, no sha,
// and the text parses back to that ref. A name the grammar cannot carry has no
// link at all.
func TestBranchLinkJSMatchesGo(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src, err := os.ReadFile(filepath.Join("static", "links.js"))
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), linksPureStart), strings.Index(string(src), linksPureEnd)
	if i < 0 || j < 0 {
		t.Fatal("links.js: the pure section is gone")
	}
	dir := t.TempDir()
	pure := filepath.Join(dir, "pure.js")
	if err := os.WriteFile(pure, []byte(string(src)[i:j]), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "check.mjs")
	body := `
import { readFileSync } from "node:fs";
const linkFor = new Function(readFileSync(process.argv[2], "utf8") + "; return linkFor;")();
const at = (name) => linkFor({ link_repo: "gigagit" }, "", { path: "", rev: name, state: "ref" });
console.log(JSON.stringify([at("main"), at("feat/x-1"), at("a:b"), at("a..b"), at("")]));
`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script, pure).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := []string{"gg://gigagit@ref:main", "gg://gigagit@ref:feat/x-1", "", "", ""}
	for n := range want {
		if got[n] != want[n] {
			t.Errorf("link %d = %q, want %q", n, got[n], want[n])
		}
	}
	for n, name := range map[int]string{0: "main", 1: "feat/x-1"} {
		l, err := model.ParseLink(got[n])
		if err != nil {
			t.Fatalf("%q does not parse: %v", got[n], err)
		}
		if l.Target.Ref != name || l.Path != "" {
			t.Errorf("%q parses to ref %q path %q", got[n], l.Target.Ref, l.Path)
		}
	}
}

// The branch menu's copy rows sit together, in the TUI's order.
func TestBranchMenuCopyRowsOrder(t *testing.T) {
	t.Parallel()
	menu := jsFunc(t, "sidebar.js", "showBranchMenu")
	last := -1
	for _, row := range []string{`"copy branch name"`, `"copy commit id"`, `branchLinkRows(b)`, `"copy commit sha"`, `"copy worktree absolute path"`} {
		at := strings.Index(menu, row)
		if at < 0 {
			t.Fatalf("branch menu has no %s", row)
		}
		if at < last {
			t.Errorf("%s is out of order", row)
		}
		last = at
	}
}

// Copy commit sha asks for the branch's full id when it is clicked. The name is
// a BRANCH: a tag of the same name, which plain rev-parse prefers, is not it.
func TestBranchShaIsTheBranchTip(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	tip := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	old := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD~1"))
	gitRun(t, dir, "branch", "same", tip)
	gitRun(t, dir, "tag", "same", old)
	ts := serve(t, New(domain.Open(dir)))

	get := func(name string) (int, string) {
		resp, err := ts.Client().Get(ts.URL + "/api/branch-sha?name=" + url.QueryEscape(name))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			SHA string `json:"sha"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.SHA
	}
	if code, sha := get("same"); code != http.StatusOK || sha != tip {
		t.Errorf("same = %d %q, want the branch tip %q", code, sha, tip)
	}
	if code, _ := get("no-such-branch"); code != http.StatusNotFound {
		t.Errorf("unknown branch = %d, want 404", code)
	}
	if code, _ := get("--all"); code != http.StatusBadRequest {
		t.Errorf("a leading dash = %d, want 400", code)
	}
	if code, _ := get(""); code != http.StatusBadRequest {
		t.Errorf("no name = %d, want 400", code)
	}
}
