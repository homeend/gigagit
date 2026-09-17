package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkTo builds a local gg:// link for e's repo. The local form is
// "gg://" + the absolute checkout path (already starting with "/" here),
// then the file, then whatever target/line the caller appends.
func linkTo(e *testEnv, suffix string) string {
	return "gg://" + filepath.ToSlash(e.dir) + suffix
}

func TestLinkResolveTakesApartAFileLink(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_link_resolve", map[string]any{"link": linkTo(e, "/a.txt:2")})
	if out["path"] != "a.txt" {
		t.Errorf("path = %v, want a.txt", out["path"])
	}
	if out["state"] != "unstaged" {
		t.Errorf("state = %v, want unstaged", out["state"])
	}
	if out["line"].(float64) != 2 {
		t.Errorf("line = %v, want 2", out["line"])
	}
	if out["side"] != "new" {
		t.Errorf("side = %v, want new", out["side"])
	}
	if out["checkout"] == "" {
		t.Error("checkout must be reported")
	}
}

// A line-less link must report NO line rather than 0 — an agent has to be
// able to tell "this link names no line" from "line 0", which is why the
// field is omitempty (and why Task 1 made a line-less link legal at all).
func TestLinkResolveOmitsAnAbsentLine(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_link_resolve", map[string]any{"link": linkTo(e, "/a.txt")})
	if _, ok := out["line"]; ok {
		t.Errorf("line = %v, want the field ABSENT for a line-less link", out["line"])
	}
	if _, ok := out["side"]; ok {
		t.Errorf("side = %v, want the field absent when there is no line", out["side"])
	}
}

// The two newest shapes, which Task 2 taught the resolver. They must report
// DIFFERENTLY on one fixture: a ref carries a NAME that travels (ruling R2),
// a pair carries two full shas because a change-set is a fixed pair (R4).
func TestLinkResolveReportsRefAndPairDifferently(t *testing.T) {
	e := newTestEnv(t)
	gitRun(t, e.dir, "commit", "--allow-empty", "-m", "second")
	head := gitRun(t, e.dir, "rev-parse", "HEAD")
	prev := gitRun(t, e.dir, "rev-parse", "HEAD~1")

	ref := e.call(t, "gg_link_resolve", map[string]any{"link": linkTo(e, "@ref:main")})
	if ref["ref"] != "main" {
		t.Errorf("ref = %v, want the NAME main (it is what travels)", ref["ref"])
	}
	if ref["commit"] != head {
		t.Errorf("commit = %v, want the tip %s resolved HERE", ref["commit"], head)
	}
	if _, ok := ref["pair_a"]; ok {
		t.Error("a ref link must not report a pair")
	}

	pair := e.call(t, "gg_link_resolve", map[string]any{"link": linkTo(e, "@"+prev+".."+head)})
	if pair["pair_a"] != prev || pair["pair_b"] != head {
		t.Errorf("pair = %v..%v, want %s..%s", pair["pair_a"], pair["pair_b"], prev, head)
	}
	if pair["ref"] != nil {
		t.Errorf("ref = %v, want absent on a pair link", pair["ref"])
	}
	// They must disagree: the whole point of the pair is that it is BOUNDED
	// while the ref names a moving tip.
	if ref["ref"] == pair["ref"] && ref["pair_a"] == pair["pair_a"] {
		t.Error("the ref and pair arms reported identically — one of them is not being read")
	}
}

func TestLinkResolveCarriesTheHint(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_link_resolve", map[string]any{"link": linkTo(e, "/a.txt?bookmark=abc123")})
	if out["hint_kind"] != "bookmark" || out["hint_id"] != "abc123" {
		t.Errorf("hint = %v/%v, want bookmark/abc123", out["hint_kind"], out["hint_id"])
	}
	// A hint never moves the link: the address must be unchanged.
	if out["path"] != "a.txt" {
		t.Errorf("path = %v, want a.txt — a hint must not change WHERE the link points", out["path"])
	}
}

// An unparseable link is a TOOL error carrying the parse refusal's own text —
// never a panic and never an opaque failure, which is the shape plan 1b had
// to fix in the web arms.
func TestLinkResolveRefusesABadLink(t *testing.T) {
	e := newTestEnv(t)
	for _, tc := range []struct{ name, link, want string }{
		{"not a link", "just-a-string", "gg://"},
		{"empty", "", "link is required"},
		{"unknown hint kind", "gg:///tmp/x/a.txt?wishlist=1", "hint kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := e.callErr(t, "gg_link_resolve", map[string]any{"link": tc.link})
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error = %q, want it to mention %q", msg, tc.want)
			}
		})
	}
}

// A link naming a DIFFERENT checkout is refused: this server is rooted at one
// repo like every other tool it serves, and answering from somewhere it was
// never pointed at would be worse than saying no.
func TestLinkResolveRefusesAForeignRepo(t *testing.T) {
	e := newTestEnv(t)
	other := t.TempDir()
	gitRun(t, other, "init", "-b", "main")
	msg := e.callErr(t, "gg_link_resolve", map[string]any{"link": "gg://" + filepath.ToSlash(other) + "/a.txt:1"})
	if msg == "" {
		t.Fatal("a foreign link must be refused")
	}
}

func TestLinkListIsEmptyArrayNotNull(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_link_list", map[string]any{})
	links, ok := out["links"].([]any)
	if !ok {
		t.Fatalf("links = %#v, want an ARRAY (null breaks a client that iterates it)", out["links"])
	}
	if len(links) != 0 {
		t.Errorf("links = %v, want empty on a fresh repo", links)
	}
}

func TestLinkListReturnsTheRingNewestFirst(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.svc.RecordLink(ctx, linkTo(e, "/a.txt:1"), "file: a.txt")
	e.svc.RecordLink(ctx, linkTo(e, "/a.txt:2"), "file: a.txt#2")

	out := e.call(t, "gg_link_list", map[string]any{})
	links := out["links"].([]any)
	if len(links) != 2 {
		t.Fatalf("links = %v, want 2", links)
	}
	first := links[0].(map[string]any)
	if first["link"] != linkTo(e, "/a.txt:2") {
		t.Errorf("newest = %v, want the most recently recorded", first["link"])
	}
	if first["desc"] != "file: a.txt#2" {
		t.Errorf("desc = %v, want the label captured at copy time", first["desc"])
	}
	if first["created"] == "" {
		t.Error("created must be reported")
	}
}

func TestCompareLinksReportsChangedFiles(t *testing.T) {
	e := newTestEnv(t)
	head := gitRun(t, e.dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "add", "-A")
	gitRun(t, e.dir, "commit", "-m", "change a.txt")
	head2 := gitRun(t, e.dir, "rev-parse", "HEAD")

	out := e.call(t, "gg_compare_links", map[string]any{
		"left":  linkTo(e, "@"+head),
		"right": linkTo(e, "@"+head2),
	})
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v, want exactly a.txt", files)
	}
	f := files[0].(map[string]any)
	if f["path"] != "a.txt" || f["status"] != "M" {
		t.Errorf("file = %v, want a.txt M", f)
	}
}

// Spec §9: two links naming different repositories are refused in phase 1.
// The message must name the disagreement, not whichever half failed to
// resolve first.
func TestCompareLinksRefusesTwoRepositories(t *testing.T) {
	e := newTestEnv(t)
	other := t.TempDir()
	gitRun(t, other, "init", "-b", "main")
	msg := e.callErr(t, "gg_compare_links", map[string]any{
		"left":  linkTo(e, "/a.txt"),
		"right": "gg://" + filepath.ToSlash(other) + "/a.txt",
	})
	if !strings.Contains(msg, "different repositories") {
		t.Errorf("error = %q, want it to name the cross-repo refusal", msg)
	}
}

func TestCompareLinksRefusesABadLink(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_compare_links", map[string]any{
		"left":  "nonsense",
		"right": linkTo(e, "/a.txt"),
	})
	if !strings.Contains(msg, "left:") {
		t.Errorf("error = %q, want it to say WHICH side was bad", msg)
	}
}

// gg_compare_file gains a link side: a link carries its own path AND state,
// so it needs neither the path nor the locator every other source requires.
func TestCompareFileAcceptsALinkSide(t *testing.T) {
	e := newTestEnv(t)
	head := gitRun(t, e.dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "link", "link": linkTo(e, "/a.txt@"+head)},
		"right": map[string]any{"source": "unstaged", "path": "a.txt"},
	})
	if out["identical"] == true {
		t.Fatal("the committed and working copies differ; identical must be false")
	}
	if !strings.Contains(out["unified_diff"].(string), "changed") {
		t.Errorf("unified_diff = %q, want the working-tree change", out["unified_diff"])
	}
}

// A link that names no file cannot be ONE side of a file compare — that is
// what gg_compare_links is for. The refusal must say so rather than reading
// some arbitrary file.
func TestCompareFileRefusesAFilelessLinkSide(t *testing.T) {
	e := newTestEnv(t)
	head := gitRun(t, e.dir, "rev-parse", "HEAD")
	msg := e.callErr(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "link", "link": linkTo(e, "@"+head)},
		"right": map[string]any{"source": "unstaged", "path": "a.txt"},
	})
	if !strings.Contains(msg, "names no file") {
		t.Errorf("error = %q, want it to explain that the link names no file", msg)
	}
}

// The accepted sources are written in three places — the struct's comment,
// the switch, and the default arm's message — and they drift silently: the
// message is what a client is told, so a source the switch accepts but the
// message omits is undiscoverable. Pin all three against one list.
func TestFileSideSourcesAgreeEverywhere(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("compare.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, source := range []string{"unstaged", "staged", "commit", "shelf", "bookmark", "link"} {
		if !strings.Contains(text, `"`+source+`"`) {
			t.Errorf("source %q is not handled in compare.go", source)
		}
		// The default arm's message lists every source a client may send.
		if !strings.Contains(text, `or "link" (got %q)`) {
			t.Error(`the "source must be …" message must list "link" — a source the switch takes but the message omits cannot be discovered`)
		}
	}
	if !strings.Contains(text, "unstaged|staged|commit|shelf|bookmark|link") {
		t.Error("fileSideIn's Source comment must list every accepted source")
	}
}

// Every link tool is read-only: none of them writes to the repository, and a
// client that trusts the annotation must not be surprised.
func TestLinkToolsAreReadOnly(t *testing.T) {
	e := newTestEnv(t)
	ann := e.listTools(t)
	for _, name := range []string{"gg_link_resolve", "gg_link_list", "gg_compare_links"} {
		a, ok := ann[name]
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		if !a.ReadOnlyHint {
			t.Errorf("%s must be annotated read-only", name)
		}
	}
}

// TestCompareLinksIsTotalOverTheReversedPair is why this tool calls
// CompareSets and never CompareFiles. CompareFiles is a thin wrapper over
// git's own diff, which walks only forward (a commit, then the index, then
// the working tree); asking {left: worktree, right: commit} through it
// answered "unsupported endpoint pair". A user may name the two sides in
// whatever order they hold the links, and one compare frontend disagreeing
// with another about what may be asked is the bug plan 1b fixed.
func TestCompareLinksIsTotalOverTheReversedPair(t *testing.T) {
	e := newTestEnv(t)
	head := gitRun(t, e.dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	forward := e.call(t, "gg_compare_links", map[string]any{
		"left":  linkTo(e, "@"+head),
		"right": linkTo(e, ""),
	})
	// The REVERSED order must answer too, not refuse.
	back := e.call(t, "gg_compare_links", map[string]any{
		"left":  linkTo(e, ""),
		"right": linkTo(e, "@"+head),
	})
	fwd, bck := forward["files"].([]any), back["files"].([]any)
	if len(fwd) == 0 || len(bck) == 0 {
		t.Fatalf("forward = %v, reversed = %v — both directions must report the change", fwd, bck)
	}
	// And they must disagree about DIRECTION: the same file, opposite sense.
	ff, bf := fwd[0].(map[string]any), bck[0].(map[string]any)
	if ff["path"] != bf["path"] {
		t.Fatalf("paths differ: %v vs %v", ff["path"], bf["path"])
	}
}
