package web

import (
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// compareEntryResp is the /api/compare-entry contract these tests assert on.
type compareEntryResp struct {
	Files []struct {
		Path   string `json:"path"`
		Status string `json:"status"`
	} `json:"files"`
	Left struct {
		Spec   string `json:"spec"`
		Label  string `json:"label"`
		Hash   string `json:"hash"`
		Frozen bool   `json:"frozen"`
	} `json:"left"`
	Right struct {
		Spec  string `json:"spec"`
		Label string `json:"label"`
		Hash  string `json:"hash"`
	} `json:"right"`
	Frozen     bool   `json:"frozen"`
	FrozenNote string `json:"frozen_note"`
	Patch      string `json:"patch"`
}

// shelvedCommitRepo shelves a commit that lives on a SIDE branch: a.txt
// rewritten and b.txt added. main then moves on independently. Keeping the
// shelved commit off main's history is what lets a test delete the branch and
// gc it away — an ancestor of the tip can never be made to disappear.
func shelvedCommitRepo(t *testing.T) (dir, entryID, oldSha, newSha string) {
	t.Helper()
	isolateState(t)
	dir = t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "a.txt", "one\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "root")

	gitRun(t, dir, "checkout", "-q", "-b", "spike")
	write(t, dir, "a.txt", "two\n")
	write(t, dir, "b.txt", "new file\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "the shelved one")
	oldSha = gitRun(t, dir, "rev-parse", "HEAD")

	gitRun(t, dir, "checkout", "-q", "main")
	write(t, dir, "a.txt", "three\n")
	gitRun(t, dir, "commit", "-am", "later")
	newSha = gitRun(t, dir, "rev-parse", "HEAD")

	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/shelf", `{"sha":"`+oldSha+`","label":"spike"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("shelve: code = %d", code)
	}
	var got shList
	getJSON(t, ts, "/api/shelf", &got)
	if len(got.Entries) != 1 {
		t.Fatalf("shelf entries = %d, want 1", len(got.Entries))
	}
	return dir, got.Entries[0].ID, oldSha, newSha
}

// commitExists probes the object store directly — `git cat-file -e` exits
// non-zero for a missing object, which gitRun would turn into a test failure.
func commitExists(t *testing.T, dir, sha string) bool {
	t.Helper()
	cmd := exec.Command("git", "cat-file", "-e", sha+"^{commit}")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func compareEntryURL(store, id, sha, format string) string {
	q := url.Values{"store": {store}, "id": {id}, "sha": {sha}}
	if format != "" {
		q.Set("format", format)
	}
	return "/api/compare-entry?" + q.Encode()
}

// The live lane: while the shelved commit still exists, both sides are real
// commits and the client can reuse the ordinary hash-keyed compare.
func TestCompareEntryLiveCommit(t *testing.T) {
	dir, id, oldSha, newSha := shelvedCommitRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	var got compareEntryResp
	if code := getJSON(t, ts, compareEntryURL("shelf", id, newSha, ""), &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got.Frozen {
		t.Errorf("frozen = true while the commit still exists")
	}
	if got.Left.Spec != "commit:"+oldSha || got.Left.Hash != oldSha {
		t.Errorf("left = %+v, want the shelved commit itself", got.Left)
	}
	if got.Right.Spec != "commit:"+newSha {
		t.Errorf("right = %+v, want the live commit", got.Right)
	}
	if got.Left.Label != "spike" {
		t.Errorf("left label = %q, want the entry's own label", got.Left.Label)
	}
	paths := map[string]string{}
	for _, f := range got.Files {
		paths[f.Path] = f.Status
	}
	if paths["a.txt"] != "M" || paths["b.txt"] != "D" {
		t.Errorf("files = %+v, want a.txt modified and b.txt gone on the live side", got.Files)
	}
}

// The whole point of the shelf: the comparison still works after the original
// commit is gone. The entry falls back to its frozen copy, the file list and
// the patch both come back — and the fallback is REPORTED, because comparing
// against a snapshot is a different statement from comparing against a commit.
func TestCompareEntryFrozenAfterCommitIsGone(t *testing.T) {
	dir, id, oldSha, tip := shelvedCommitRepo(t)
	// Delete the branch that held the shelved commit and expire it out of the
	// object store: the sha stops resolving, exactly as after a real gc.
	gitRun(t, dir, "branch", "-D", "spike")
	gitRun(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitRun(t, dir, "gc", "--prune=now", "--quiet")
	if commitExists(t, dir, oldSha) {
		t.Fatalf("fixture: commit %s survived gc — the frozen lane needs it gone", oldSha)
	}
	ts := serve(t, New(domain.Open(dir)))

	var got compareEntryResp
	if code := getJSON(t, ts, compareEntryURL("shelf", id, tip, "patch"), &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !got.Frozen || got.Left.Spec != "shelf:"+id {
		t.Fatalf("left = %+v, frozen = %v; want the frozen shelf side", got.Left, got.Frozen)
	}
	if !strings.Contains(got.FrozenNote, "no longer exists") {
		t.Errorf("frozen_note = %q, want it to say the commit is gone", got.FrozenNote)
	}
	// The shelved commit changed a.txt ("one"→"two") and added b.txt; the tip
	// has a.txt at "three" and no b.txt. Scoped to the entry's own members.
	paths := map[string]string{}
	for _, f := range got.Files {
		paths[f.Path] = f.Status
	}
	if paths["a.txt"] != "M" {
		t.Errorf("files = %+v, want a.txt modified", got.Files)
	}
	if paths["b.txt"] != "D" {
		t.Errorf("files = %+v, want b.txt reported as absent on the live side", got.Files)
	}
	if !strings.Contains(got.Patch, "a/a.txt") || !strings.Contains(got.Patch, "+three") {
		t.Errorf("patch does not diff the frozen side against the tip:\n%s", got.Patch)
	}
}

// A file entry has no commit to compare trees with; the refusal says so
// rather than producing an empty comparison.
func TestCompareEntryRefusesFileEntry(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	write(t, dir, "f.txt", "edited\n")
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/shelf", `{"path":"f.txt","state":"unstaged"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("shelve file: failed")
	}
	var list shList
	getJSON(t, ts, "/api/shelf", &list)
	sha := gitRun(t, dir, "rev-parse", "HEAD")

	code := getJSON(t, ts, compareEntryURL("shelf", list.Entries[0].ID, sha, ""), nil)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", code)
	}
}

// Guards: the store name is an allowlist, the sha is hex-only, and comparing
// an entry with its own commit is refused instead of showing an empty diff.
func TestCompareEntryGuards(t *testing.T) {
	dir, id, oldSha, newSha := shelvedCommitRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	if code := getJSON(t, ts, compareEntryURL("elsewhere", id, newSha, ""), nil); code != http.StatusBadRequest {
		t.Errorf("unknown store: status = %d, want 400", code)
	}
	if code := getJSON(t, ts, compareEntryURL("shelf", id, "main", ""), nil); code != http.StatusBadRequest {
		t.Errorf("branch name as sha: status = %d, want 400", code)
	}
	if code := getJSON(t, ts, compareEntryURL("shelf", "nope", newSha, ""), nil); code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", code)
	}
	if code := getJSON(t, ts, compareEntryURL("shelf", id, oldSha, ""), nil); code != http.StatusUnprocessableEntity {
		t.Errorf("self-compare: status = %d, want 422", code)
	}
}

// A commit BOOKMARK compares live. It stores no blobs, so a gone commit has
// no fallback — and that must be said, not silently returned as empty.
func TestCompareEntryCommitBookmark(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 3)
	old := gitRun(t, dir, "rev-parse", "HEAD~2")
	tip := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/bookmarks", `{"sha":"`+old+`","label":"before"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("bookmark: failed")
	}
	var bms bmList
	getJSON(t, ts, "/api/bookmarks", &bms)

	var got compareEntryResp
	if code := getJSON(t, ts, compareEntryURL("bookmarks", bms.Entries[0].ID, tip, ""), &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got.Frozen || got.Left.Hash != old || got.Left.Label != "before" {
		t.Errorf("left = %+v, frozen = %v", got.Left, got.Frozen)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "f.txt" {
		t.Errorf("files = %+v, want f.txt", got.Files)
	}
}

// --- /api/entry-diff --------------------------------------------------------

type entryDiffResp struct {
	Rows []struct {
		Kind  string `json:"kind"`
		Left  string `json:"left"`
		Right string `json:"right"`
	} `json:"rows"`
}

func entryDiffURL(left, right, path, status string) string {
	q := url.Values{"left": {left}, "right": {right}, "path": {path}}
	if status != "" {
		q.Set("status", status)
	}
	return "/api/entry-diff?" + q.Encode()
}

// A shelved FILE against the working tree: the frozen bytes on the left, what
// is on disk now on the right. This is the "compare a file against a stored
// copy" lane.
func TestEntryDiffShelvedFileAgainstWorktree(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	write(t, dir, "f.txt", "shelved version\n")
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/shelf", `{"path":"f.txt","state":"unstaged"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("shelve: failed")
	}
	var list shList
	getJSON(t, ts, "/api/shelf", &list)
	// Move on: the working file now says something else.
	write(t, dir, "f.txt", "current version\n")

	var got entryDiffResp
	code := getJSON(t, ts, entryDiffURL("shelf:"+list.Entries[0].ID, "worktree", "f.txt", ""), &got)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	var left, right []string
	for _, r := range got.Rows {
		if r.Left != "" {
			left = append(left, r.Left)
		}
		if r.Right != "" {
			right = append(right, r.Right)
		}
	}
	if !strings.Contains(strings.Join(left, "\n"), "shelved version") {
		t.Errorf("left side = %v, want the frozen bytes", left)
	}
	if !strings.Contains(strings.Join(right, "\n"), "current version") {
		t.Errorf("right side = %v, want the working-tree bytes", right)
	}
}

// The frozen whole-tree lane's per-file diffs: shelf member ↔ live commit.
func TestEntryDiffShelfMemberAgainstCommit(t *testing.T) {
	dir, id, _, newSha := shelvedCommitRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	var got entryDiffResp
	if code := getJSON(t, ts, entryDiffURL("shelf:"+id, "commit:"+newSha, "a.txt", ""), &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	joined := ""
	for _, r := range got.Rows {
		joined += r.Left + "|" + r.Right + "\n"
	}
	if !strings.Contains(joined, "two") || !strings.Contains(joined, "three") {
		t.Errorf("rows do not show two→three:\n%s", joined)
	}
}

// A file BOOKMARK is an address, not a copy: it resolves to whatever is there
// now. Comparing the staged copy against the working one through two bookmarks
// is the shape that proves both halves of that — and neither may be cached.
func TestEntryDiffFileBookmarkResolvesLive(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	write(t, dir, "f.txt", "staged version\n")
	gitRun(t, dir, "add", "f.txt")
	write(t, dir, "f.txt", "working version\n")
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/bookmarks", `{"path":"f.txt","state":"staged","label":"as staged"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("bookmark: failed")
	}
	var bms bmList
	getJSON(t, ts, "/api/bookmarks", &bms)

	var got entryDiffResp
	if code := getJSON(t, ts, entryDiffURL("bookmark:"+bms.Entries[0].ID, "worktree", "f.txt", ""), &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	joined := ""
	for _, r := range got.Rows {
		joined += r.Left + "|" + r.Right + "\n"
	}
	if !strings.Contains(joined, "staged version") || !strings.Contains(joined, "working version") {
		t.Errorf("rows do not show the staged copy against the working one:\n%s", joined)
	}

	// A LIVE side must not be cached: editing the file and asking again has to
	// show the new bytes, or the compare would go stale under the user.
	write(t, dir, "f.txt", "edited again\n")
	got = entryDiffResp{}
	getJSON(t, ts, entryDiffURL("bookmark:"+bms.Entries[0].ID, "worktree", "f.txt", ""), &got)
	fresh := ""
	for _, r := range got.Rows {
		fresh += r.Right
	}
	if !strings.Contains(fresh, "edited again") {
		t.Errorf("second read served a cached diff: %q", fresh)
	}
}

// A COMMIT bookmark in the file lane is its commit — and it keeps no blobs, so
// once that commit is gone there is nothing to fall back on. The refusal says
// which commit rather than returning an empty diff.
func TestEntryDiffCommitBookmarkWhoseCommitIsGone(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-q", "-b", "gone")
	write(t, dir, "f.txt", "on the doomed branch\n")
	gitRun(t, dir, "commit", "-qam", "doomed")
	doomed := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "main")
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/bookmarks", `{"sha":"`+doomed+`","label":"doomed"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("bookmark: failed")
	}
	var bms bmList
	getJSON(t, ts, "/api/bookmarks", &bms)

	gitRun(t, dir, "branch", "-D", "gone")
	gitRun(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitRun(t, dir, "gc", "--prune=now", "--quiet")
	if commitExists(t, dir, doomed) {
		t.Fatalf("fixture: %s survived gc", doomed)
	}
	code := getJSON(t, ts, entryDiffURL("bookmark:"+bms.Entries[0].ID, "worktree", "f.txt", ""), nil)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", code)
	}
}

// The side vocabulary is closed, and a path is always required: a client
// cannot name something outside it and have the server guess.
func TestEntryDiffRejectsUnknownSides(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := getJSON(t, ts, entryDiffURL("hocus:1", "worktree", "f.txt", ""), nil); code != http.StatusBadRequest {
		t.Errorf("unknown side: status = %d, want 400", code)
	}
	if code := getJSON(t, ts, entryDiffURL("commit:main", "worktree", "f.txt", ""), nil); code != http.StatusBadRequest {
		t.Errorf("branch name as a commit side: status = %d, want 400", code)
	}
	if code := getJSON(t, ts, entryDiffURL("worktree", "staged", "", ""), nil); code != http.StatusBadRequest {
		t.Errorf("missing path: status = %d, want 400", code)
	}
	if code := getJSON(t, ts, entryDiffURL("shelf:nope", "worktree", "f.txt", ""), nil); code != http.StatusNotFound {
		t.Errorf("unknown shelf id: status = %d, want 404", code)
	}
}
