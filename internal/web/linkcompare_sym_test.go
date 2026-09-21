package web

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

type symRowGot struct {
	Path      string `json:"path"`
	Left      string `json:"left"`
	Right     string `json:"right"`
	Differs   bool   `json:"differs"`
	LeftSpec  string `json:"left_spec"`
	RightSpec string `json:"right_spec"`
}

type linkCmpSym struct {
	Files []linkCmpFile
	Sym   []symRowGot
}

// twoAttemptsRepo: main, and two branches off it that are two attempts at one
// job — they change diff.txt differently and same.txt identically, only A
// deletes gone.txt, and each adds a file of its own.
func twoAttemptsRepo(t *testing.T) (dir, base, a, b string) {
	t.Helper()
	isolateState(t)
	dir = newRepoDir(t, 1)
	put := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"diff.txt", "same.txt", "gone.txt"} {
		put(f, "base\n")
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	base = gitRun(t, dir, "rev-parse", "HEAD")

	gitRun(t, dir, "checkout", "-q", "-b", "agent-a")
	put("diff.txt", "from a\n")
	put("same.txt", "agreed\n")
	put("onlya.txt", "a\n")
	gitRun(t, dir, "rm", "-q", "gone.txt")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "attempt a")
	a = gitRun(t, dir, "rev-parse", "HEAD")

	gitRun(t, dir, "checkout", "-q", "-b", "agent-b", base)
	put("diff.txt", "from b\n")
	put("same.txt", "agreed\n")
	put("onlyb.txt", "b\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "attempt b")
	b = gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "main")
	return dir, base, a, b
}

// Two BOUNDED sets answer the aligned rows beside the plain listing: every
// member of either set, each side saying absent / present / deleted.
func TestCompareLinksAnswersTheAlignedRows(t *testing.T) {
	dir, base, a, b := twoAttemptsRepo(t)
	ts := linkServe(t, dir)
	var got linkCmpSym
	left, right := localLink(dir, "", pairTarget(base, a)), localLink(dir, "", pairTarget(base, b))
	if code := getAny(t, ts, cmpURL(left, right), &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	want := []symRowGot{
		{Path: "diff.txt", Left: "present", Right: "present", Differs: true},
		{Path: "gone.txt", Left: "deleted", Right: "absent"},
		{Path: "onlya.txt", Left: "present", Right: "absent", Differs: true},
		{Path: "onlyb.txt", Left: "absent", Right: "present", Differs: true},
		{Path: "same.txt", Left: "present", Right: "present"},
	}
	if !reflect.DeepEqual(got.Sym, want) {
		t.Fatalf("sym:\n got %+v\nwant %+v", got.Sym, want)
	}
	// The plain listing is untouched: the three rows that differ, as before.
	var paths []string
	for _, f := range got.Files {
		paths = append(paths, f.Status+" "+f.Path)
	}
	if !reflect.DeepEqual(paths, []string{"M diff.txt", "D onlya.txt", "A onlyb.txt"}) {
		t.Fatalf("files = %v", paths)
	}
}

// An unbounded side has no member list to align, and a PAIR LANDING is a
// commit diff with a note scope, not two sets: neither offers the view.
func TestCompareLinksOffersNoAlignedRowsWithoutTwoSets(t *testing.T) {
	dir, base, a, _ := twoAttemptsRepo(t)
	ts := linkServe(t, dir)
	for name, path := range map[string]string{
		"a point side": cmpURL(localLink(dir, "", commitTarget(base)), localLink(dir, "", pairTarget(base, a))),
		"pair landing": "/api/compare-links?" + url.Values{"a": {base}, "b": {a}}.Encode(),
	} {
		var raw map[string]any
		if code := getAny(t, ts, path, &raw); code != 200 {
			t.Fatalf("%s: status %d", name, code)
		}
		if _, has := raw["sym"]; has {
			t.Fatalf("%s: must not carry sym: %v", name, raw["sym"])
		}
	}
}

// Each side says what KIND of thing its link names, so the page can title the
// screen ("preview comparison") without parsing a link — which it cannot.
func TestCompareLinksNamesEachSidesKind(t *testing.T) {
	dir, base, a, _ := twoAttemptsRepo(t)
	ts := linkServe(t, dir)
	var got struct{ Left, Right struct{ Kind string } }
	preview := localLink(dir, "", model.LinkTarget{State: model.StateCommitted, Preview: &model.LinkPreview{Source: "agent-b", Target: "main"}})
	if code := getAny(t, ts, cmpURL(localLink(dir, "", pairTarget(base, a)), preview), &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if got.Left.Kind != "pair" || got.Right.Kind != "preview" {
		t.Fatalf("kinds = %q / %q, want pair / preview", got.Left.Kind, got.Right.Kind)
	}
}

// /api/link-base DESCRIBES the link it classifies: the dialog's field holds two
// forty-digit ids, and the words are what tell the user which change it is. A
// landing hint names where the link was copied from.
func TestLinkBaseDescribesTheLink(t *testing.T) {
	dir, base, a, _ := twoAttemptsRepo(t)
	srv, svc := linkServer(t, dir)
	ts := serve(t, srv)
	saved, err := svc.PairAdd(context.Background(), base, a, "attempt a, reviewed")
	if err != nil {
		t.Fatal(err)
	}
	type answer struct{ Kind, Desc, Origin string }
	ask := func(link string) answer {
		t.Helper()
		var got answer
		if code := getAny(t, ts, "/api/link-base?"+url.Values{"link": {link}}.Encode(), &got); code != 200 {
			t.Fatalf("%s: status %d", link, code)
		}
		return got
	}
	link := localLink(dir, "", pairTarget(base, a))
	// Copied off the saved Previews row: the entry's LABEL, and where it came from.
	if got := ask(link + "?preview=" + saved.ID); got.Desc != "pair: attempt a, reviewed" || got.Origin != "copied from a saved preview" {
		t.Fatalf("a hinted link names its entry and its origin: %+v", got)
	}
	// The bare link still describes (domain's own words for it) and has no origin.
	if got := ask(link); got.Desc == "" || got.Origin != "" {
		t.Fatalf("a bare link: %+v", got)
	}
	// A preview link is a bound kind of its own and describes as the branch pair.
	preview := localLink(dir, "", model.LinkTarget{State: model.StateCommitted, Preview: &model.LinkPreview{Source: "agent-b", Target: "main"}})
	if got := ask(preview); got.Desc != "preview: main...agent-b" {
		t.Fatalf("a preview link: %+v", got)
	}
	// A link still being typed describes nothing, and is not an error.
	if got := ask("gg://nope@"); got.Desc != "" || got.Kind != "none" {
		t.Fatalf("an unparseable link: %+v", got)
	}
}
