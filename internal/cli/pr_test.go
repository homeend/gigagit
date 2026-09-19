package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge/forgetest"
)

// prRepo builds a repo whose .git/fakegh holds the named forge fixtures and
// points gg at the fake gh. Every test using it is serial: it sets process env.
func prRepo(t *testing.T, fixtures ...string) string {
	t.Helper()
	dir := newRepoDir(t)
	t.Setenv(forgetest.EnvBin, forgetest.BuildFakeGH(t))
	t.Setenv(forgetest.EnvFixtures, "") // the fake falls back to <cwd>/.git/fakegh
	files := map[string]string{}
	for _, name := range fixtures {
		b, err := os.ReadFile(filepath.Join("..", "forge", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(b)
	}
	forgetest.Seed(t, filepath.Join(dir, ".git", "fakegh"), files)
	return dir
}

func runPR(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(dir, append([]string{"pr"}, args...), strings.NewReader(""), &out, &errb, "")
	return out.String(), errb.String(), code
}

func TestPRList(t *testing.T) {
	dir := prRepo(t, "pr-list.json")
	out, errs, code := runPR(t, dir, "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"#7", "alice", "feat/forge → main", "[changes_requested]", "Add forge tab",
		"#9", "[draft]", "bob:fix/typo → main"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output lacks %q:\n%s", want, out)
		}
	}
	out, _, _ = runPR(t, dir, "list", "--json")
	var prs []map[string]any
	if err := json.Unmarshal([]byte(out), &prs); err != nil || len(prs) != 2 || prs[0]["number"] == nil || prs[0]["head_sha"] == nil {
		t.Errorf("json = %s (%v)", out, err)
	}
}

func TestPRViewAndComments(t *testing.T) {
	dir := prRepo(t, "pr-list.json", "pr-view-7.json", "threads-7.json")
	out, errs, code := runPR(t, dir, "view", "7")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"#7", "merged", "https://github.com/homeend/gigagit/pull/7", "Second paragraph.",
		"── conversation ──", "bob: nice work", "carol [changes_requested]: needs the rename", "dave [approved]",
		"── outdated ──", "a.go (old) ghost: old remark", "-old"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("view must say the comment list was truncated:\n%s", out)
	}
	out, _, _ = runPR(t, dir, "comments", "#7")
	for _, want := range []string{"a.go:10-12 (new) carol: rename this", "  alice: done", "b.go (file) carol: split this file [resolved]"} {
		if !strings.Contains(out, want) {
			t.Errorf("comments lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old remark") {
		t.Error("comments must not print outdated threads inline")
	}
	out, _, code = runPR(t, dir, "comments", "7", "--json")
	var cs map[string]any
	if err := json.Unmarshal([]byte(out), &cs); code != 0 || err != nil || cs["inline"] == nil || cs["truncated"] != true {
		t.Errorf("comments json = %s (%v)", out, err)
	}
}

func TestPRWithoutAForgeFailsLoudly(t *testing.T) {
	dir := prRepo(t) // fake gh, NO fixtures → detection fails
	out, errs, code := runPR(t, dir, "list")
	if code != 1 || out != "" || !strings.Contains(errs, "gg pr:") || !strings.Contains(errs, "no forge CLI") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestPRViewUnknownNumber(t *testing.T) {
	dir := prRepo(t, "pr-list.json")
	_, errs, code := runPR(t, dir, "view", "99")
	if code != 1 || !strings.Contains(errs, "not found") {
		t.Errorf("exit %d, stderr %q", code, errs)
	}
}

func TestPRUsage(t *testing.T) {
	dir := prRepo(t, "pr-list.json")
	for _, args := range [][]string{{}, {"view"}, {"view", "x"}, {"bogus"}, {"fetch", "0"}, {"list", "extra"}} {
		if _, _, code := runPR(t, dir, args...); code != 2 {
			t.Errorf("gg pr %v → exit %d, want 2", args, code)
		}
	}
}

func TestPRForgetWithoutARefIsANoOp(t *testing.T) {
	dir := prRepo(t, "pr-list.json")
	out, errs, code := runPR(t, dir, "forget", "7")
	if code != 0 || !strings.Contains(out, "was not fetched") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
}
