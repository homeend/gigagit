package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// Entry ↔ entry: the direction the TUI reaches from the bookmark and shelf
// popups themselves. The right side is a second stored entry rather than a
// live commit, and every combination of stores has to work — the two stores
// address the same commits by different routes.

func entryPairURL(store, id, rightStore, rightID, format string) string {
	q := url.Values{
		"store": {store}, "id": {id},
		"right_store": {rightStore}, "right_id": {rightID},
	}
	if format != "" {
		q.Set("format", format)
	}
	return "/api/compare-entry?" + q.Encode()
}

// shelveCommit shelves sha and returns the new entry's id.
func shelveCommit(t *testing.T, ts *httptest.Server, sha, label string) string {
	t.Helper()
	if code := postJSON(t, ts, "/api/shelf", `{"sha":"`+sha+`","label":"`+label+`"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("shelve %s: code = %d", label, code)
	}
	var list shList
	getJSON(t, ts, "/api/shelf", &list)
	for _, e := range list.Entries {
		if e.Label == label {
			return e.ID
		}
	}
	t.Fatalf("shelved entry %q not in the list", label)
	return ""
}

// bookmarkCommit bookmarks sha and returns the new entry's id.
func bookmarkCommit(t *testing.T, ts *httptest.Server, sha, label string) string {
	t.Helper()
	if code := postJSON(t, ts, "/api/bookmarks", `{"sha":"`+sha+`","label":"`+label+`"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("bookmark %s: code = %d", label, code)
	}
	var list bmList
	getJSON(t, ts, "/api/bookmarks", &list)
	for _, e := range list.Entries {
		if e.Label == label {
			return e.ID
		}
	}
	t.Fatalf("bookmark %q not in the list", label)
	return ""
}

// Two entries, both still live: the pair resolves to two real commits, which
// is what lets the client reuse the ordinary hash-keyed compare view.
func TestCompareEntryPairBothLive(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 3)
	older := gitRun(t, dir, "rev-parse", "HEAD~2")
	newer := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	bmID := bookmarkCommit(t, ts, older, "the before")
	shID := shelveCommit(t, ts, newer, "the after")

	var got compareEntryResp
	if code := getJSON(t, ts, entryPairURL("bookmarks", bmID, "shelf", shID, ""), &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got.Frozen {
		t.Errorf("frozen = true while both commits exist")
	}
	if got.Left.Hash != older || got.Right.Hash != newer {
		t.Errorf("left/right = %s/%s, want %s/%s", got.Left.Hash, got.Right.Hash, older, newer)
	}
	if got.Left.Label != "the before" || got.Right.Label != "the after" {
		t.Errorf("labels = %q/%q, want each entry's own", got.Left.Label, got.Right.Label)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "f.txt" {
		t.Errorf("files = %+v, want f.txt", got.Files)
	}
}

// The order is the caller's: FIRST side is left/older, so swapping the two
// arguments has to swap the sides rather than being normalized away.
func TestCompareEntryPairKeepsTheGivenOrder(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 3)
	older := gitRun(t, dir, "rev-parse", "HEAD~2")
	newer := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	a := bookmarkCommit(t, ts, older, "a")
	b := bookmarkCommit(t, ts, newer, "b")

	var fwd, rev compareEntryResp
	getJSON(t, ts, entryPairURL("bookmarks", a, "bookmarks", b, ""), &fwd)
	getJSON(t, ts, entryPairURL("bookmarks", b, "bookmarks", a, ""), &rev)
	if fwd.Left.Hash != older || rev.Left.Hash != newer {
		t.Errorf("left hashes = %s / %s, want %s / %s (order preserved)", fwd.Left.Hash, rev.Left.Hash, older, newer)
	}
}

// The headline case for the shelf: two entries whose commit is GONE. Each
// side falls back to its own frozen copy, independently, and both fallbacks
// are named.
func TestCompareEntryPairBothFrozen(t *testing.T) {
	isolateState(t)
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "a.txt", "root\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "root")

	// Two doomed commits on two side branches, each changing a.txt.
	gitRun(t, dir, "checkout", "-q", "-b", "one")
	write(t, dir, "a.txt", "first spike\n")
	gitRun(t, dir, "commit", "-qam", "spike one")
	shaOne := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "main")
	gitRun(t, dir, "checkout", "-q", "-b", "two")
	write(t, dir, "a.txt", "second spike\n")
	gitRun(t, dir, "commit", "-qam", "spike two")
	shaTwo := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "main")

	ts := serve(t, New(domain.Open(dir)))
	idOne := shelveCommit(t, ts, shaOne, "spike one")
	idTwo := shelveCommit(t, ts, shaTwo, "spike two")

	gitRun(t, dir, "branch", "-D", "one")
	gitRun(t, dir, "branch", "-D", "two")
	gitRun(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitRun(t, dir, "gc", "--prune=now", "--quiet")
	if commitExists(t, dir, shaOne) || commitExists(t, dir, shaTwo) {
		t.Fatal("fixture: a doomed commit survived gc")
	}

	var got compareEntryResp
	if code := getJSON(t, ts, entryPairURL("shelf", idOne, "shelf", idTwo, "patch"), &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !got.Frozen || !got.Left.Frozen || !got.Right.Frozen {
		t.Fatalf("frozen flags = %v/%v/%v, want all true", got.Frozen, got.Left.Frozen, got.Right.Frozen)
	}
	if got.Left.Spec != "shelf:"+idOne || got.Right.Spec != "shelf:"+idTwo {
		t.Errorf("specs = %s / %s", got.Left.Spec, got.Right.Spec)
	}
	// BOTH fallbacks are named — "one of these is a snapshot" is a different
	// warning from "both are".
	for _, sha := range []string{shortShaOf(shaOne), shortShaOf(shaTwo)} {
		if !strings.Contains(got.FrozenNote, sha) {
			t.Errorf("frozen_note = %q, want it to name %s", got.FrozenNote, sha)
		}
	}
	if len(got.Files) != 1 || got.Files[0].Path != "a.txt" || got.Files[0].Status != "M" {
		t.Errorf("files = %+v, want a.txt modified between the two frozen copies", got.Files)
	}
	if !strings.Contains(got.Patch, "first spike") || !strings.Contains(got.Patch, "second spike") {
		t.Errorf("patch does not show both frozen sides:\n%s", got.Patch)
	}
}

// A mixed pair — one side gc'd, one alive — composes: frozen ↔ live lands in
// domain's shelf↔commit lane rather than being refused.
func TestCompareEntryPairMixedFrozenAndLive(t *testing.T) {
	dir, frozenID, goneSha, tip := shelvedCommitRepo(t)
	gitRun(t, dir, "branch", "-D", "spike")
	gitRun(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitRun(t, dir, "gc", "--prune=now", "--quiet")
	if commitExists(t, dir, goneSha) {
		t.Fatalf("fixture: %s survived gc", goneSha)
	}
	ts := serve(t, New(domain.Open(dir)))
	liveID := bookmarkCommit(t, ts, tip, "still here")

	var got compareEntryResp
	if code := getJSON(t, ts, entryPairURL("shelf", frozenID, "bookmarks", liveID, ""), &got); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !got.Frozen || !got.Left.Frozen || got.Right.Frozen {
		t.Fatalf("want left frozen and right live, got %v/%v", got.Left, got.Right)
	}
	if got.Right.Hash != tip {
		t.Errorf("right hash = %s, want the live tip %s", got.Right.Hash, tip)
	}
	if strings.Count(got.FrozenNote, "no longer exists") != 1 {
		t.Errorf("frozen_note = %q, want exactly one fallback named", got.FrozenNote)
	}
}

// Two entries recording the SAME commit are a non-comparison: the answer
// would be an empty file list indistinguishable from "these are identical",
// which is true but useless.
func TestCompareEntryPairSameCommitRefused(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	bmA := bookmarkCommit(t, ts, sha, "one")
	bmB := bookmarkCommit(t, ts, sha, "two")

	if code := getJSON(t, ts, entryPairURL("bookmarks", bmA, "bookmarks", bmB, ""), nil); code != http.StatusUnprocessableEntity {
		t.Errorf("two bookmarks of one commit: status = %d, want 422", code)
	}
}

// …with one exception, carried over from the TUI (startEntryCompare's
// distinctShelves): two DIFFERENT shelf entries of the same commit each froze
// their own copy of the files, and those copies can differ.
//
// The shelf derives an entry id from the commit and its content, so shelving
// one commit twice returns the SAME entry — the exception is unreachable
// through this API today. The rule is still implemented, because the id
// scheme is the store's business and not this endpoint's to depend on; the
// test says out loud when it cannot exercise it rather than quietly passing.
func TestCompareEntryPairDistinctShelvesAllowed(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	shA := shelveCommit(t, ts, sha, "snap one")
	shB := shelveCommit(t, ts, sha, "snap two")
	if shA == shB {
		t.Skip("the shelf deduplicates two snapshots of one commit into a single entry")
	}
	if code := getJSON(t, ts, entryPairURL("shelf", shA, "shelf", shB, ""), nil); code != http.StatusOK {
		t.Errorf("two shelf entries of one commit: status = %d, want 200", code)
	}
}

// A FILE entry has no commit to compare trees with, whichever side it is on.
func TestCompareEntryPairRefusesFileEntry(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 2)
	write(t, dir, "f.txt", "edited\n")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	postJSON(t, ts, "/api/shelf", `{"path":"f.txt","state":"unstaged","label":"a file"}`, "application/json", "", nil)
	var list shList
	getJSON(t, ts, "/api/shelf", &list)
	fileID := list.Entries[0].ID
	commitID := bookmarkCommit(t, ts, sha, "a commit")

	if code := getJSON(t, ts, entryPairURL("shelf", fileID, "bookmarks", commitID, ""), nil); code != http.StatusUnprocessableEntity {
		t.Errorf("file entry on the left: status = %d, want 422", code)
	}
	if code := getJSON(t, ts, entryPairURL("bookmarks", commitID, "shelf", fileID, ""), nil); code != http.StatusUnprocessableEntity {
		t.Errorf("file entry on the right: status = %d, want 422", code)
	}
}

// The right side's store is an allowlist too, and a missing right side is a
// request for nothing rather than a comparison against HEAD.
func TestCompareEntryPairRightSideGuards(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	id := bookmarkCommit(t, ts, sha, "one")

	if code := getJSON(t, ts, entryPairURL("bookmarks", id, "elsewhere", "x", ""), nil); code != http.StatusBadRequest {
		t.Errorf("unknown right store: status = %d, want 400", code)
	}
	if code := getJSON(t, ts, entryPairURL("bookmarks", id, "shelf", "nope", ""), nil); code != http.StatusNotFound {
		t.Errorf("unknown right id: status = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/compare-entry?store=bookmarks&id="+id, nil); code != http.StatusBadRequest {
		t.Errorf("no right side at all: status = %d, want 400", code)
	}
}

func shortShaOf(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
