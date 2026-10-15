package domain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

func readJSON(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-notes-store", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// The fixture's reviews read through gg: the commit review and the PR
// review, each with its parsed document; the working review belongs to
// another checkout and is not listed here.
func TestFixtureReviewsReadThroughGG(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	svc.UseNotesDir(fixtureStoreDir(t))
	rs, err := svc.Reviews(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Review{}
	for _, r := range rs {
		byID[r.ID] = r
	}
	c, ok := byID["c0ffee01"]
	if !ok || c.Doc == nil || c.Scope != fixtureCommit+"^.."+fixtureCommit || c.Branch != "main" || c.Preview != "" {
		t.Fatalf("commit review = %+v (found %v)", c, ok)
	}
	if n, f := c.Doc.NoteCount(); n != 2 || f != 1 {
		t.Fatalf("commit review remarks = %d on %d files, want 2 on 1", n, f)
	}
	if c.Doc.Files[0].Notes[1].Side != "old" || c.Doc.Files[0].Notes[1].Range != [2]int{2, 2} {
		t.Fatalf("old-side remark = %+v", c.Doc.Files[0].Notes[1])
	}
	p, ok := byID["c0ffee02"]
	if !ok || p.Doc == nil || p.Preview != "main...refs/gg/pr/7" || p.Scope != fixturePRBase+".."+fixturePRHead || p.Commit != fixturePRHead {
		t.Fatalf("PR review = %+v (found %v)", p, ok)
	}
	if _, ok := byID["bb220002"]; ok {
		t.Fatal("another checkout's working review was listed")
	}
}

// Review focus 4: the reply stored in previews.toml threads under the PR
// review's remark 0 — never as a note of its own.
func TestFixtureReplyThreadsUnderItsRoot(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	svc.UseNotesDir(fixtureStoreDir(t))
	ctx := context.Background()
	r, err := svc.Review(ctx, "c0ffee02")
	if err != nil {
		t.Fatal(err)
	}
	// Review.Replies holds the review's stored replies (remarkThreadSet.attach);
	// a remark reply names its remark.
	if len(r.Replies) != 1 || r.Replies[0].ID != "c0ffee03" || r.Replies[0].Remark != "review:c0ffee02:0" ||
		r.Replies[0].RemarkFP != remarkFP("internal/x/gate.go", model.NoteSideNew, [2]int{1, 1}, "Name the parameter.") {
		t.Fatalf("replies = %+v, want c0ffee03 on remark review:c0ffee02:0", r.Replies)
	}
	// And NotesAt on the PR head's file lists no root named c0ffee03.
	rs, err := svc.NotesAt(ctx, model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead, Path: "internal/x/gate.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range rs {
		if n.Note.ID == "c0ffee03" {
			t.Fatal("the remark reply surfaced as a root note")
		}
	}
}

// The committed line note, its reply and its resolution read through gg's
// store and threaded by gg's resolver. The fixture commit is not in the
// test repository, so the note is ORPHANED (status, never dropped by the
// store); NotesAt would hide it for a file view.
func TestFixtureLineNoteThreadReadThroughGG(t *testing.T) {
	t.Parallel()
	st := notes.NewFileStore(fixtureStoreDir(t))
	all, err := st.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	var mine []model.Note
	for _, n := range all {
		if n.Address.Commit == fixtureCommit && n.Address.Path == "internal/x/gate.go" && !n.IsReply() || n.ParentID == "aa110001" {
			mine = append(mine, n)
		}
	}
	var root *ResolvedNote
	rs := resolveNotes(mine, nil, nil)
	for i := range rs {
		if rs[i].Note.ID == "aa110001" {
			root = &rs[i]
		}
	}
	if root == nil {
		t.Fatalf("line note aa110001 not found in %+v", mine)
	}
	if root.Status != model.NoteOrphaned {
		t.Fatalf("status = %s, want orphaned (the commit is not in this repository)", root.Status)
	}
	if len(root.Replies) != 1 || root.Replies[0].Note.ID != "aa110002" || root.Replies[0].Note.Link == "" {
		t.Fatalf("thread = %+v", root.Replies)
	}
	if len(root.Note.Meta) != 1 || root.Note.Meta[0].Key != "kind" || len(root.Note.Added) != 1 || root.Note.Added[0].Value != "agreed" {
		t.Fatalf("meta/added = %+v / %+v", root.Note.Meta, root.Note.Added)
	}
	res, err := st.LoadAllResolved()
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Root != "aa110001" || res[0].By != "me" {
		t.Fatalf("resolutions = %+v, want aa110001 resolved by me", res)
	}
}

// vectors.json: the two fingerprints and the keys, computed by gg's own
// functions — a reader ports them and must get the same strings.
func TestFixtureVectors(t *testing.T) {
	t.Parallel()
	var v struct {
		ContextHash []struct {
			Lines []string `json:"lines"`
			Hash  string   `json:"hash"`
		} `json:"context_hash"`
		RemarkFP []struct {
			Path    string `json:"path"`
			Side    string `json:"side"`
			Range   [2]int `json:"range"`
			Summary string `json:"summary"`
			FP      string `json:"fp"`
		} `json:"remark_fp"`
		RepoKey []struct {
			CommonDir string `json:"common_dir"`
			Key       string `json:"key"`
		} `json:"repo_key"`
		WorktreeKey []struct {
			Top string `json:"top"`
			Key string `json:"key"`
		} `json:"worktree_key"`
	}
	readJSON(t, "vectors.json", &v)
	for _, c := range v.ContextHash {
		if got := model.NoteContextHash(c.Lines); got != c.Hash {
			t.Errorf("context_hash(%q) = %s, want %s", c.Lines, got, c.Hash)
		}
	}
	for _, c := range v.RemarkFP {
		if got := remarkFP(c.Path, model.NoteSide(c.Side), c.Range, c.Summary); got != c.FP {
			t.Errorf("remark_fp(%q,%s,%v,%q) = %s, want %s", c.Path, c.Side, c.Range, c.Summary, got, c.FP)
		}
	}
	for _, c := range v.RepoKey {
		if got := repoKey(c.CommonDir); got != c.Key && filepath.Separator == '/' {
			t.Errorf("repo_key(%q) = %s, want %s", c.CommonDir, got, c.Key)
		}
	}
	for _, c := range v.WorktreeKey {
		if got := strings.TrimPrefix(string(notes.WorktreePart(c.Top)), "wt-"); got != c.Key {
			t.Errorf("worktree_key(%q) = %s, want %s", c.Top, got, c.Key)
		}
	}
}

// relocation-cases.json: gg's resolver on each case (spec part 2 §5.2);
// a reader runs the same file against its port.
func TestRelocationCases(t *testing.T) {
	t.Parallel()
	var cases []struct {
		Name     string   `json:"name"`
		Lines    []string `json:"lines"`
		Range    [2]int   `json:"range"`
		Anchored []string `json:"anchored"`
		Status   string   `json:"status"`
		At       [2]int   `json:"at"`
	}
	readJSON(t, "relocation-cases.json", &cases)
	for _, c := range cases {
		n := model.Note{Range: c.Range, Address: model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit, Path: "f"}}
		if c.Range == [2]int{0, 0} {
			n.Address.Path = "" // entry-level
		}
		if len(c.Anchored) > 0 {
			n.ContextHash = model.NoteContextHash(c.Anchored)
		}
		st, at := resolveOne(n, c.Lines)
		if string(st) != c.Status || at != c.At {
			t.Errorf("%s: got %s at %v, want %s at %v", c.Name, st, at, c.Status, c.At)
		}
	}
}
