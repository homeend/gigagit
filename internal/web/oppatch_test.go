package web

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// getRaw GETs path and returns the status, headers and the body BYTES. The
// shared getJSON helper decodes, which is exactly what a patch download must
// not go through.
func getRaw(t *testing.T, ts *httptest.Server, path string) (int, http.Header, []byte) {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header, body
}

// A commit downloads as a git-am-able mailbox: the `From <sha>` header line
// git format-patch writes, the commit's subject, and its diff.
func TestCommitPatchDownload(t *testing.T) {
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	code, hdr, body := getRaw(t, ts, "/api/commit-patch?sha="+sha)
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (%s)", code, body)
	}
	if !bytes.HasPrefix(body, []byte("From ")) {
		t.Errorf("body starts %q, want a format-patch mailbox", firstLine(body))
	}
	if !bytes.Contains(body, []byte("Subject: [PATCH] c2")) {
		t.Errorf("patch does not carry the commit's subject:\n%s", firstLine(body))
	}
	if !bytes.Contains(body, []byte("+content 2")) {
		t.Error("patch does not carry the commit's diff")
	}
	// The filename is the short sha, and it must arrive as an ATTACHMENT —
	// without this header the browser renders the patch as a page instead of
	// saving it.
	if got, want := hdr.Get("Content-Disposition"), `attachment; filename="`+sha[:7]+`.patch"`; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	if got := hdr.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
}

// One file's change inside a commit, with the file's base name in the download
// name so two patches off the same commit do not collide.
func TestFilePatchDownload(t *testing.T) {
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "two files")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	code, hdr, body := getRaw(t, ts, "/api/commit-patch?sha="+sha+"&path=a.txt")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (%s)", code, body)
	}
	if !bytes.Contains(body, []byte("+alpha")) {
		t.Error("patch does not carry a.txt's change")
	}
	if bytes.Contains(body, []byte("+beta")) {
		t.Error("patch carries b.txt's change too — the path was ignored")
	}
	if got, want := hdr.Get("Content-Disposition"), `attachment; filename="`+sha[:7]+`-a.txt.patch"`; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
}

// A merge commit has no single patch: `git format-patch -1` silently emits a
// DIFFERENT commit's, so it is refused up front — 422 with that sentence, not
// a plausible-looking wrong download.
func TestCommitPatchRefusesMerge(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side")
	gitRun(t, dir, "checkout", "main")
	gitRun(t, dir, "merge", "--no-ff", "-m", "merge side", "side")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	code, hdr, body := getRaw(t, ts, "/api/commit-patch?sha="+sha)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422 (%s)", code, body)
	}
	if !strings.Contains(string(body), "merge commit") {
		t.Errorf("body = %s, want the merge refusal's own sentence", body)
	}
	if hdr.Get("Content-Disposition") != "" {
		t.Error("a refusal must not offer itself as a download")
	}
}

// A sha reaches git argv, so anything that is not a plain object name is
// refused before a verb sees it — and so is an option-looking path.
func TestCommitPatchRejectsBadArgs(t *testing.T) {
	dir := newRepoDir(t, 1)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	for _, c := range []struct{ name, query string }{
		{"no sha", ""},
		{"not hex", "?sha=HEAD"},
		{"rev expression", "?sha=" + sha + "~1"},
		{"option-shaped path", "?sha=" + sha + "&path=--output=/tmp/x"},
	} {
		code, _, _ := getRaw(t, ts, "/api/commit-patch"+c.query)
		if code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", c.name, code)
		}
	}
}

// The round trip the feature exists for: export a commit, then apply the file
// back on a branch that does not have it, and the commit is there again.
func TestApplyPatchRecreatesTheCommit(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("from side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side work")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "main")
	ts := serve(t, New(domain.Open(dir)))

	_, _, patch := getRaw(t, ts, "/api/commit-patch?sha="+sha)
	file := filepath.Join(t.TempDir(), "0001-side-work.patch")
	if err := os.WriteFile(file, patch, 0o644); err != nil {
		t.Fatal(err)
	}

	// mode "commits" is the git am lane. The client sends no mode at all and
	// lets the engine park its own question; the explicit value is what that
	// decision resolves to, and is the one worth asserting end to end.
	body := `{"op":"apply-patch","path":` + jsonString(file) + `,"mode":"commits"}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if subj := gitRun(t, dir, "log", "-1", "--format=%s", "main"); subj != "side work" {
		t.Errorf("main tip = %q, want the applied commit", subj)
	}
	if _, err := os.Stat(filepath.Join(dir, "side.txt")); err != nil {
		t.Errorf("the patch's file is not in the working tree: %v", err)
	}
}

// A plain `git diff` is not a mailbox. Asked for the commits lane it is
// refused by the engine in its own words — over the event stream, so the user
// reads why rather than seeing a failed op with no reason.
func TestApplyPatchPlainDiffRefusedAsCommits(t *testing.T) {
	dir := newRepoDir(t, 2)
	ts := serve(t, New(domain.Open(dir)))

	file := filepath.Join(t.TempDir(), "plain.diff")
	if err := os.WriteFile(file, []byte("diff --git a/f.txt b/f.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"op":"apply-patch","path":` + jsonString(file) + `,"mode":"commits"}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 60*time.Second)
	done := events[len(events)-1]
	if done["ok"] == true {
		t.Fatalf("a plain diff was accepted as a mailbox: %v", done)
	}
	if msg, _ := done["error"].(string); !strings.Contains(msg, "mailbox") {
		t.Errorf("error = %q, want the engine's not-a-mailbox sentence", msg)
	}
}

// A path that is not there fails as a failed OPERATION carrying the engine's
// own message, not as an HTTP 500 with nothing to read.
func TestApplyPatchMissingFile(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	missing := filepath.Join(t.TempDir(), "not-here.patch")
	body := `{"op":"apply-patch","path":` + jsonString(missing) + `}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 60*time.Second)
	done := events[len(events)-1]
	if done["ok"] == true {
		t.Fatalf("a missing patch file was accepted: %v", done)
	}
	if msg, _ := done["error"].(string); !strings.Contains(msg, "read patch") {
		t.Errorf("error = %q, want the engine's read-patch failure", msg)
	}
}

// Refusals decided before anything runs: no path, an option-shaped path, an
// unknown mode.
func TestApplyPatchGuards(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"no path", `{"op":"apply-patch"}`, http.StatusBadRequest},
		{"option-shaped path", `{"op":"apply-patch","path":"--exclude=x"}`, http.StatusBadRequest},
		{"unknown mode", `{"op":"apply-patch","path":"x.patch","mode":"rebase"}`, http.StatusBadRequest},
	} {
		if code := postJSON(t, ts, "/api/op", c.body, "application/json", "", nil); code != c.want {
			t.Errorf("%s: code = %d, want %d", c.name, code, c.want)
		}
	}
}

// Copy to a directory outside the repository: the prefill names it, the op
// writes the entry's files there.
func TestExportEntryToDir(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	id := addShelfFile(t, ts, "notes.txt")

	var dest struct {
		Dir   string `json:"dir"`
		Files int    `json:"files"`
	}
	if code := getJSON(t, ts, "/api/export-dest?store=shelf&id="+id, &dest); code != http.StatusOK {
		t.Fatalf("export-dest: code = %d", code)
	}
	// The default is anchored on the MAIN worktree, beside the repository —
	// never inside it, which is what makes this different from a restore.
	if !strings.HasPrefix(dest.Dir, filepath.Clean(dir)+".tmp") {
		t.Errorf("dir = %q, want a path under %q", dest.Dir, filepath.Clean(dir)+".tmp")
	}
	if dest.Files != 1 {
		t.Errorf("files = %d, want 1", dest.Files)
	}

	target := filepath.Join(t.TempDir(), "copied")
	body := `{"op":"export-to-dir","store":"shelf","id":"` + id + `","dest":` + jsonString(target) + `}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(target, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep me\n" {
		t.Errorf("copied = %q, want the shelved bytes", got)
	}
}

// A commit BOOKMARK exports the files that commit changes — live, since a
// bookmark is a reference and not a snapshot.
func TestExportCommitBookmarkToDir(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	var out struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if code := postJSON(t, ts, "/api/bookmarks", `{"sha":"`+sha+`","label":"tip"}`, "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("bookmark add: code = %d", code)
	}

	target := filepath.Join(t.TempDir(), "commit-copy")
	body := `{"op":"export-to-dir","store":"bookmarks","id":"` + out.Entry.ID + `","dest":` + jsonString(target) + `}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "content 2" {
		t.Errorf("copied = %q, want the commit's version of f.txt", got)
	}
}

// Refusals for the copy lane, decided before the op starts.
func TestExportToDirGuards(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "n.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	id := addShelfFile(t, ts, "n.txt")

	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"no dest", `{"op":"export-to-dir","store":"shelf","id":"` + id + `"}`, http.StatusBadRequest},
		{"no id", `{"op":"export-to-dir","store":"shelf","dest":"/tmp/x"}`, http.StatusBadRequest},
		{"unknown store", `{"op":"export-to-dir","store":"attic","id":"` + id + `","dest":"/tmp/x"}`, http.StatusBadRequest},
		{"unknown entry", `{"op":"export-to-dir","store":"shelf","id":"nope","dest":"/tmp/x"}`, http.StatusNotFound},
	} {
		if code := postJSON(t, ts, "/api/op", c.body, "application/json", "", nil); code != c.want {
			t.Errorf("%s: code = %d, want %d", c.name, code, c.want)
		}
	}
	// The prefill endpoint answers with the same refusals as the op.
	if code := getJSON(t, ts, "/api/export-dest?store=shelf&id=nope", nil); code != http.StatusNotFound {
		t.Errorf("export-dest unknown entry: code = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/export-dest?store=attic&id="+id, nil); code != http.StatusBadRequest {
		t.Errorf("export-dest unknown store: code = %d, want 400", code)
	}
}

// firstLine is the head of a body, for a readable failure message.
func firstLine(b []byte) []byte {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[:i]
	}
	if len(b) > 120 {
		return b[:120]
	}
	return b
}
