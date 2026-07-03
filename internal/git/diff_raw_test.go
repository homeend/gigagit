package git

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func TestDiffNumstatArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff", gitexec.Result{})
	r := &Repo{Runner: f}
	spec := model.DiffSpec{Cached: true, Rev: "main..HEAD", Paths: []string{"a.go", "b.go"}}
	if _, err := r.DiffNumstat(context.Background(), spec); err != nil {
		t.Fatalf("DiffNumstat: %v", err)
	}
	want := []string{"diff", "--numstat", "-z", "--cached", "main..HEAD", "--", "a.go", "b.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestDiffPatchArgvMinimal(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff", gitexec.Result{Stdout: "PATCH"})
	r := &Repo{Runner: f}
	out, err := r.DiffPatch(context.Background(), model.DiffSpec{})
	if err != nil || out != "PATCH" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	want := []string{"diff"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestParseNumstat(t *testing.T) {
	// Real -z shapes (verified empirically): ordinary "A\tD\tpath\x00",
	// rename "A\tD\t\x00old\x00new\x00", binary "-\t-\tpath\x00".
	in := "3\t1\tmain.go\x00-\t-\timg.png\x001\t0\t\x00old.go\x00new.go\x00"
	got := ParseNumstat(in)
	want := []model.DiffStat{
		{Path: "main.go", Added: 3, Deleted: 1},
		{Path: "img.png", Binary: true},
		{Path: "new.go", OldPath: "old.go", Added: 1, Deleted: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseNumstatEmpty(t *testing.T) {
	if got := ParseNumstat(""); len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

func TestParseNumstatTruncatedRename(t *testing.T) {
	// A rename record cut off before its two path fields must not panic.
	if got := ParseNumstat("1\t0\t\x00old.go"); len(got) != 1 || got[0].Path != "old.go" || got[0].OldPath != "" {
		// acceptable: the lone trailing field is dropped OR treated as
		// incomplete; the invariant under test is "no panic, no bogus entry
		// with empty Path".
		for _, s := range got {
			if s.Path == "" {
				t.Fatalf("entry with empty path: %+v", got)
			}
		}
	}
}
