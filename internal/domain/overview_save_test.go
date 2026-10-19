package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// permOvRepo: main with a.go (5 lines) and b.go (3 lines), committed on
// branch main; the notes store in a temp dir.
func permOvRepo(t *testing.T) (string, *Service) {
	t.Helper()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "a.go", "1\n2\n3\n4\n5\n", "a")
	commitFile(t, dir, "b.go", "x\ny\nz\n", "b")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	return dir, svc
}

func ovDoc(title, overview, files string) []byte {
	if files == "" {
		files = "[]"
	}
	return []byte(`{"version":1,"title":"` + title + `","overview":"` + overview + `","files":` + files + `}`)
}

func saveOv(t *testing.T, svc *Service, doc []byte) (OverviewSaved, error) {
	t.Helper()
	tg, err := svc.HeadOverviewTarget(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.SaveOverview(context.Background(), SaveOverview{Target: tg, Agent: "Claude Code", Text: doc})
	return got, err
}

func problems(t *testing.T, err error) []string {
	t.Helper()
	var r *OverviewRefused
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want *OverviewRefused", err)
	}
	return r.Problems
}

func TestSaveOverviewStoresOnHeadWithBranch(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	got, err := saveOv(t, svc, ovDoc("Locks", "[three](a.go:3) [y](b.go:2) [file](b.go)",
		`[{"path":"a.go","annotations":[{"newRange":[2,4],"summary":"the gate"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	ov, err := svc.Overview(context.Background(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Commit != revParse(t, dir, "HEAD") || ov.Branch != "main" || ov.Title != "Locks" || ov.Agent != "Claude Code" || ov.Kind != ReviewOnBranch {
		t.Fatalf("overview = %+v", ov)
	}
}

func TestSaveOverviewRefusesEveryProblemAtOnce(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	sha7 := revParse(t, dir, "HEAD")[:7]
	_, err := saveOv(t, svc, ovDoc("T", "[n](note:t3) [gone](zzz.go:1) [far](a.go:9) [range](b.go:2-7)",
		`[{"path":"a.go","annotations":[{"newRange":[6,6],"summary":"past"}]}]`))
	want := []string{
		"note:t3: a note link — link the lines instead",
		"zzz.go: not in " + sha7,
		"a.go:9: past the end (5 lines at " + sha7 + ")",
		"b.go:2-7: past the end (3 lines at " + sha7 + ")",
		"a.go:6: past the end (5 lines at " + sha7 + ")",
	}
	if got := problems(t, err); !slices.Equal(got, want) {
		t.Fatalf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if all, _ := svc.Overviews(context.Background()); len(all) != 0 {
		t.Fatalf("a refused save stored %d overviews", len(all))
	}
}

func TestSaveOverviewRefusesUncommittedFiles(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		dirty func(dir string)
	}{
		{"modified", func(dir string) { writeFile(t, dir, "a.go", "1\n2\nX\n4\n5\n") }},
		{"staged", func(dir string) { writeFile(t, dir, "a.go", "1\n2\nX\n4\n5\n"); runGitIn(t, dir, "add", "a.go") }},
		{"deleted", func(dir string) { _ = os.Remove(filepath.Join(dir, "a.go")) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir, svc := permOvRepo(t)
			c.dirty(dir)
			_, err := saveOv(t, svc, ovDoc("T", "[three](a.go:3) [y](b.go:2)", ""))
			if got := problems(t, err); !slices.Equal(got, []string{"a.go: uncommitted changes"}) {
				t.Fatalf("problems = %v", got)
			}
		})
	}
}

func TestSaveOverviewOlderCommitDiffersOnDisk(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	old := revParse(t, dir, "HEAD")
	commitFile(t, dir, "a.go", "1\n2\nNEW\n4\n5\n", "a changed")
	got, _, err := svc.SaveOverview(context.Background(), SaveOverview{
		Target: OverviewTarget{Commit: old}, Agent: "c", Text: ovDoc("T", "[three](a.go:3) [y](b.go:2)", "")})
	if p := problems(t, err); !slices.Equal(p, []string{"a.go: differs from " + old[:7] + " on disk"}) {
		t.Fatalf("problems = %v (saved %+v)", p, got)
	}
}

func TestSaveOverviewReplacesByTitle(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	first, err := saveOv(t, svc, ovDoc("Locks", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	second, err := saveOv(t, svc, ovDoc("  Locks ", "[b](b.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.Replaced {
		t.Fatalf("second = %+v, want a replace of %s", second, first.ID)
	}
	other, err := saveOv(t, svc, ovDoc("Parsing", "[a](a.go:1)", ""))
	if err != nil || other.ID == first.ID || other.Replaced {
		t.Fatalf("other = %+v %v, want a new overview", other, err)
	}
	all, _ := svc.Overviews(context.Background())
	if len(all) != 2 {
		t.Fatalf("overviews = %d, want 2", len(all))
	}
}

func TestSaveOverviewDetachedHead(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	runGitIn(t, dir, "checkout", "-q", "--detach")
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	ov, _ := svc.Overview(context.Background(), got.ID)
	if ov.Branch != "" || ov.Kind != ReviewOnCommit {
		t.Fatalf("detached overview = %+v", ov)
	}
}

func TestSaveOverviewOddPaths(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	sha7 := revParse(t, dir, "HEAD")[:7]
	// ../a.go is no anchor (plain text); ./ is dropped from an anchor and
	// from a remark's path alike, so both name a.go — and a.go is stored.
	got, err := saveOv(t, svc, ovDoc("T", "[up](../a.go:1) [dot](./a.go:1) [ok](a.go:2)",
		`[{"path":"./a.go","annotations":[{"newRange":[1,1],"summary":"r"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Doc.Files) != 1 || got.Doc.Files[0].Path != "a.go" {
		t.Fatalf("stored files = %+v", got.Doc.Files)
	}
	// a remark on ../a.go is still refused
	_, err = saveOv(t, svc, ovDoc("T2", "[ok](a.go:2)",
		`[{"path":"../a.go","annotations":[{"newRange":[1,1],"summary":"r"}]}]`))
	if p := problems(t, err); !slices.Equal(p, []string{"../a.go: not in " + sha7}) {
		t.Fatalf("problems = %v", p)
	}
}

func TestSaveOverviewReadsEachFileOnce(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	sha7 := revParse(t, dir, "HEAD")[:7]
	_, err := saveOv(t, svc, ovDoc("T", "[1](a.go:1) [2](a.go:2-3) [3](a.go:4) [bad](a.go:6)", ""))
	if got := problems(t, err); !slices.Equal(got, []string{"a.go:6: past the end (5 lines at " + sha7 + ")"}) {
		t.Fatalf("problems = %v", got)
	}
}

func TestSaveOverviewRefusesNonCommitLinks(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	ctx := context.Background()
	for name, res := range map[string]Resolved{
		"working": {Addr: model.FileAddress{State: model.StateUnstaged, Path: "a.go"}},
		"staged":  {Addr: model.FileAddress{State: model.StateStaged}},
		"pair":    {Pair: &model.LinkPair{A: "a", B: "b"}, Addr: model.FileAddress{State: model.StateCommitted}, Commit: "b"},
	} {
		if _, err := svc.LinkOverviewTarget(ctx, res); !errors.Is(err, ErrOverviewTarget) {
			t.Errorf("%s: err = %v, want ErrOverviewTarget", name, err)
		}
	}
}

func TestSaveOverviewOnAPullRequest(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t) // PR #7: feat changes big.go lines 5 and 25; main checked out
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := svc.LinkOverviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Commit != head || tg.PR != 7 || tg.Branch != "" || !strings.HasSuffix(tg.Scope, git.PRRef(7)) {
		t.Fatalf("target = %+v", tg)
	}
	// big.go differs from main's on disk — a PR skips the disk check.
	got, _, err := svc.SaveOverview(ctx, SaveOverview{Target: tg, Agent: "c", Text: ovDoc("How #7 works", "[changed](big.go:25)", "")})
	if err != nil {
		t.Fatal(err)
	}
	ov, _ := svc.Overview(ctx, got.ID)
	if ov.Scope != tg.Scope || ov.Commit != head {
		t.Fatalf("stored = %+v", ov)
	}
}

func TestSaveOverviewDryRunStoresNothing(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	tg, _ := svc.HeadOverviewTarget(context.Background())
	got, _, err := svc.SaveOverview(context.Background(), SaveOverview{Target: tg, Agent: "c", Text: ovDoc("T", "[a](a.go:1)", ""), DryRun: true})
	if err != nil || got.ID != "" || got.Doc.Title != "T" {
		t.Fatalf("dry run = %+v %v", got, err)
	}
	if all, _ := svc.Overviews(context.Background()); len(all) != 0 {
		t.Fatal("dry run stored an overview")
	}
}

// An anchor wrapped in backticks is a code span, not a link: the stored
// overview would open nothing there, so the save is refused with the fix.
// A saved overview reports how many anchors it has (0 = the CLI warns).
func TestSaveOverviewRefusesBacktickedAnchors(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	_, err := saveOv(t, svc, ovDoc("T", "Before `[the gate](a.go:2-4)` and [`ok`](b.go:1) and `arr[i](x)`, again `[the gate](a.go:2-4)`.", ""))
	want := []string{"`[the gate](a.go:2-4)`: a link inside backticks is plain text, not an anchor — write [the gate](a.go:2-4) (for a code look, put the backticks inside the brackets)"}
	if got := problems(t, err); !slices.Equal(got, want) {
		t.Fatalf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	got, err := saveOv(t, svc, ovDoc("T", "[`ok`](b.go:1) and [two](a.go:2)", ""))
	if err != nil || got.Anchors != 2 {
		t.Fatalf("save = %+v, %v; want 2 anchors", got, err)
	}
	if got, err := saveOv(t, svc, ovDoc("U", "Only words.", "")); err != nil || got.Anchors != 0 {
		t.Fatalf("save = %+v, %v; want 0 anchors", got, err)
	}
}
