package notes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

func reviewRoot(id, commit string) model.Note {
	return model.Note{ID: id, Source: model.NoteSourceAgent, Tags: []string{model.ReviewTag},
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit},
		Side:    model.NoteSideNew, Summary: "Review: x", Created: time.Unix(100, 0).UTC()}
}

func remarkReply(id, reviewID string, n int, commit string) model.Note {
	return model.Note{ID: id, ParentID: reviewID, Remark: fmt.Sprintf("review:%s:%d", reviewID, n), Source: model.NoteSourceAgent,
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit},
		Side:    model.NoteSideNew, Summary: "agreed", RemarkFP: "fp", RemarkSummary: "s",
		Created: time.Unix(200, 0).UTC()}
}

func lineNote(id, commit, path string) model.Note {
	return model.Note{ID: id, Source: model.NoteSourceUser,
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: path},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, ContextHash: "h", Summary: "line",
		Created: time.Unix(300, 0).UTC()}
}

func putAll(t *testing.T, st *FileStore, ns ...model.Note) {
	t.Helper()
	for _, n := range ns {
		if err := st.Put(n); err != nil {
			t.Fatalf("Put %s: %v", n.ID, err)
		}
	}
}

func TestRemarkReplySurvivesOtherWrites(t *testing.T) {
	t.Parallel()
	st := NewFileStore(t.TempDir())
	c := strings.Repeat("a", 40)
	putAll(t, st, reviewRoot("rev00001", c), remarkReply("rep00001", "rev00001", 0, c))
	// Any later write in the part runs the orphan prune.
	putAll(t, st, lineNote("line0001", strings.Repeat("b", 40), "x.go"))
	ns, err := st.Load(PartCommits)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(ns, func(n model.Note) bool { return n.ID == "rep00001" }) {
		t.Fatalf("the remark reply was pruned: %+v", ns)
	}
}

func TestRemovingAReviewTakesItsRemarkReplies(t *testing.T) {
	t.Parallel()
	st := NewFileStore(t.TempDir())
	c := strings.Repeat("a", 40)
	putAll(t, st, reviewRoot("rev00001", c), remarkReply("rep00001", "rev00001", 0, c), lineNote("line0001", c, "x.go"))
	if err := st.Remove("rev00001"); err != nil {
		t.Fatal(err)
	}
	ns, _ := st.Load(PartCommits)
	if len(ns) != 1 || ns[0].ID != "line0001" {
		t.Fatalf("after removing the review: %+v", ns)
	}
}

func TestResolutionsRoundTripAndRoute(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := NewFileStore(root)
	c := strings.Repeat("a", 40)
	putAll(t, st, reviewRoot("rev00001", c), lineNote("line0001", c, "x.go"))
	at := time.Unix(500, 0).UTC()
	if err := st.Resolve(model.ThreadResolution{Root: "review:rev00001:2", By: "B", At: at, RemarkFP: "fp"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Resolve(model.ThreadResolution{Root: "line0001", By: "me", At: at}); err != nil {
		t.Fatal(err)
	}
	// Replace, not duplicate.
	if err := st.Resolve(model.ThreadResolution{Root: "line0001", By: "you", At: at}); err != nil {
		t.Fatal(err)
	}
	rs, err := st.LoadResolved(PartCommits)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[1].By != "you" {
		t.Fatalf("resolutions = %+v", rs)
	}
	if err := st.Resolve(model.ThreadResolution{Root: "nope0000", At: at}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolving an unknown root: %v", err)
	}
	if err := st.Unresolve("line0001"); err != nil {
		t.Fatal(err)
	}
	if err := st.Unresolve("line0001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second unresolve: %v", err)
	}
	all, _ := st.LoadAllResolved()
	if len(all) != 1 || all[0].Root != "review:rev00001:2" {
		t.Fatalf("LoadAllResolved = %+v", all)
	}
	// The table is in the part file itself.
	data, _ := os.ReadFile(filepath.Join(root, "commits.toml"))
	if !strings.Contains(string(data), "[[resolved]]") {
		t.Fatalf("no [[resolved]] table:\n%s", data)
	}
}

func TestResolutionsArePrunedWithTheirRoot(t *testing.T) {
	t.Parallel()
	st := NewFileStore(t.TempDir())
	c := strings.Repeat("a", 40)
	putAll(t, st, reviewRoot("rev00001", c), lineNote("line0001", c, "x.go"), lineNote("line0002", c, "y.go"))
	at := time.Unix(500, 0).UTC()
	if err := st.Resolve(model.ThreadResolution{Root: "review:rev00001:0", At: at, RemarkFP: "fp"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Resolve(model.ThreadResolution{Root: "line0001", At: at}); err != nil {
		t.Fatal(err)
	}
	if err := st.Remove("rev00001"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sweep(func(n model.Note) bool { return n.ID != "line0001" }); err != nil {
		t.Fatal(err)
	}
	if rs, _ := st.LoadAllResolved(); len(rs) != 0 {
		t.Fatalf("orphaned resolutions kept: %+v", rs)
	}
}

func TestEmptyResolvedTableIsNotWritten(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := NewFileStore(root)
	putAll(t, st, lineNote("line0001", strings.Repeat("a", 40), "x.go"))
	data, _ := os.ReadFile(filepath.Join(root, "commits.toml"))
	if strings.Contains(string(data), "resolved") {
		t.Fatalf("an empty resolved table was written:\n%s", data)
	}
}

func TestConcurrentResolvesBothLand(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	c := strings.Repeat("a", 40)
	putAll(t, NewFileStore(root), lineNote("line0001", c, "x.go"), lineNote("line0002", c, "y.go"))
	var wg sync.WaitGroup
	for _, id := range []string{"line0001", "line0002"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Two stores = two processes' views of one directory.
			if err := NewFileStore(root).Resolve(model.ThreadResolution{Root: id, At: time.Unix(1, 0).UTC()}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if rs, _ := NewFileStore(root).LoadAllResolved(); len(rs) != 2 {
		t.Fatalf("resolutions = %+v", rs)
	}
}
