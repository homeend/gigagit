package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// sessOvWorktrees is a repo plus a second worktree whose session docs hold a
// note on a.go:3 and an overview anchoring a.go:3-4 and that note. It
// returns the main checkout, the second one, the overview id and its note.
func sessOvWorktrees(t *testing.T) (main, wt2, id, noteID string) {
	t.Helper()
	repo := overviewCLIRepo(t)
	main, _ = filepath.EvalSymlinks(repo)
	wt2 = filepath.Join(filepath.Dir(main), filepath.Base(main)+"-wt2")
	gitRun(t, main, "worktree", "add", "-q", wt2)
	t.Cleanup(func() { os.RemoveAll(wt2) })
	svc2 := domain.Open(wt2)
	ctx := context.Background()
	cd, err := svc2.GitCommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inbox := config.SessionSteerDir(cd, wt2)
	_, out, errb := runNote(t, svc2, inbox, "add", "a.go:3", "--summary", "the gate", "--rationale", "because")
	noteID = notedID(t, out+errb)
	code, out, errb := runOverview(t, svc2, inbox, "See [gate](a.go:3-4) and [why](note:"+noteID+").\n", "add", "--title", "Tour", "--background")
	if code != 0 {
		t.Fatalf("overview add = %d %q", code, errb)
	}
	id = strings.Fields(out)[0]
	return main, wt2, id, noteID
}

func TestOverviewShowSessionLinkFromAnotherWorktree(t *testing.T) {
	t.Parallel()
	main, wt2, id, noteID := sessOvWorktrees(t)
	link, err := domain.SessionOverviewLink(wt2, id)
	if err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, main, "overview", "show", link)
	slash := filepath.ToSlash(wt2)
	if code != 0 || !strings.Contains(out, "Tour") || !strings.Contains(out, "See [gate](a.go:3-4)") ||
		!strings.Contains(out, slash+"/a.go:3-4") || !strings.Contains(out, "note "+noteID) || !strings.Contains(out, "the gate") {
		t.Fatalf("show = %d out=%q err=%q", code, out, errb)
	}
	code, out, errb = runCLI(t, main, "overview", "show", link, "--json")
	if code != 0 {
		t.Fatalf("show --json = %d %q", code, errb)
	}
	var got struct {
		Kind, Link, Title, Text string
		Anchors                 []struct {
			Label, Path, Link string
			Note              *struct{ ID, Summary string }
		}
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json %q: %v", out, err)
	}
	if got.Kind != "session" || got.Link != link || got.Title != "Tour" || len(got.Anchors) != 2 ||
		!strings.Contains(got.Anchors[0].Link, slash+"/a.go") || got.Anchors[1].Note == nil || got.Anchors[1].Note.Summary != "the gate" {
		t.Fatalf("json = %+v", got)
	}
}

func TestOverviewShowSessionLinkGone(t *testing.T) {
	t.Parallel()
	main, wt2, _, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, "20200101T000000-0000")
	code, _, errb := runCLI(t, main, "overview", "show", link)
	if code != 1 || !strings.Contains(errb, "overview 20200101T000000-0000 no longer exists") {
		t.Fatalf("show = %d %q", code, errb)
	}
}

func TestOverviewShowPermanentListsAnchorLinks(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	code, out, errb := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "A", "--stdin")
	if code != 0 {
		t.Fatalf("save = %d %q", code, errb)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	link := strings.TrimSpace(lines[len(lines)-1])
	code, out, _ = runCLI(t, dir, "overview", "show", link)
	if code != 0 || !strings.Contains(out, "anchors:") || !strings.Contains(out, "/a.go@") && !strings.Contains(out, "a.go:3") {
		t.Fatalf("show = %d %q", code, out)
	}
	if !strings.Contains(out, "three → gg://") {
		t.Fatalf("no anchor link line: %q", out)
	}
}

func TestOverviewShowReviewLink(t *testing.T) {
	t.Parallel()
	dir, plink := reviewSaveRepo(t)
	doc := `{"version":1,"summary":"fine","overview":"Start at [second](f.txt:2).","files":[{"path":"f.txt","annotations":[{"newRange":[2,2],"summary":"one"}]}]}`
	code, out, errb := runCLIStdin(t, dir, doc, "review", "save", plink, "--agent", "Rev", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("review save = %d %q", code, errb)
	}
	rlink := jsonField(t, out, "link")
	code, out, errb = runCLI(t, dir, "overview", "show", rlink)
	if code != 0 || !strings.Contains(out, "review by Rev") || !strings.Contains(out, "Start at [second](f.txt:2).") || !strings.Contains(out, "second → gg://") {
		t.Fatalf("show = %d out=%q err=%q", code, out, errb)
	}
	code, out, _ = runCLI(t, dir, "overview", "show", rlink, "--json")
	if code != 0 || jsonField(t, out, "kind") != "review" {
		t.Fatalf("show --json = %d %q", code, out)
	}
}

func TestOverviewShowReviewLinkWithoutOverview(t *testing.T) {
	t.Parallel()
	dir, plink := reviewSaveRepo(t)
	doc := `{"version":1,"summary":"fine","files":[{"path":"f.txt","annotations":[{"newRange":[2,2],"summary":"one"}]}]}`
	code, out, errb := runCLIStdin(t, dir, doc, "review", "save", plink, "--agent", "Rev", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("review save = %d %q", code, errb)
	}
	id, rlink := jsonField(t, out, "id"), jsonField(t, out, "link")
	code, _, errb = runCLI(t, dir, "overview", "show", rlink)
	if code != 1 || !strings.Contains(errb, "review "+id+" has no overview — read it with gg review show <link>") {
		t.Fatalf("show = %d %q", code, errb)
	}
}

func TestHintBlindVerbsRefuseSessionOverviewLink(t *testing.T) {
	t.Parallel()
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	for _, args := range [][]string{
		{"diff", link},
		{"note", "add", link, "--summary", "x"},
		{"link", "text", link},
		{"overview", "export", link},
	} {
		code, _, errb := runCLI(t, main, args...)
		if code == 0 || !strings.Contains(errb, "gg overview show") {
			t.Errorf("%v = %d %q", args, code, errb)
		}
	}
}

func TestLinkResolveSessionOverview(t *testing.T) {
	t.Parallel()
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	code, out, errb := runCLI(t, main, "link", "resolve", link)
	if code != 0 || !strings.Contains(out, wt2) || !strings.Contains(out, "overview "+id) {
		t.Fatalf("resolve = %d out=%q err=%q", code, out, errb)
	}
}
