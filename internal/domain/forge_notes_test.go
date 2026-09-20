package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

func prSet(n string) PreviewNoteSet {
	return PreviewNoteSet{Source: "refs/gg/pr/" + n, Target: "main", Tip: "tip", Base: "base"}
}

func reviewThreads() []model.ForgeComment {
	t0 := time.Unix(1_700_000_000, 0)
	return []model.ForgeComment{
		{ID: "C1", Kind: model.ForgeCommentInline, Author: "carol", Path: "a.go", Side: model.NoteSideNew,
			Line: 12, StartLine: 10, Body: "rename this\r\n\r\nit shadows the package", Resolved: true, Created: t0, Updated: t0},
		{ID: "C2", ParentID: "C1", Kind: model.ForgeCommentInline, Author: "alice", Path: "a.go", Side: model.NoteSideNew,
			Line: 12, Body: "done", Created: t0.Add(time.Hour), Updated: t0.Add(time.Hour)},
		{ID: "C3", Kind: model.ForgeCommentInline, Author: "bob", Path: "a.go", Side: model.NoteSideOld,
			Line: 3, Body: "why was this removed?", Created: t0, Updated: t0},
		{ID: "C4", Kind: model.ForgeCommentFile, Author: "dave", Path: "b.txt", Body: "should this file exist?", Created: t0, Updated: t0},
		{ID: "C5", Kind: model.ForgeCommentInline, Author: "erin", Path: "a.go", Outdated: true, Line: 0, Body: "old", Created: t0, Updated: t0},
		{ID: "G1", Kind: model.ForgeCommentGeneral, Author: "bob", Body: "nice", Created: t0, Updated: t0},
	}
}

func TestForgeNotesConversion(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{comments: reviewThreads()}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	if got := svc.forgeNotesFor(prSet("7"), "a.go"); got != nil {
		t.Fatalf("reads never fetch: want nothing before a refresh, got %d", len(got))
	}
	if ff.commentCalls != 0 {
		t.Fatalf("a read called the provider %d times", ff.commentCalls)
	}
	if changed, err := svc.PRCommentsRefresh(ctx, 7); err != nil || !changed {
		t.Fatalf("first refresh: changed=%v err=%v", changed, err)
	}
	got := svc.forgeNotesFor(prSet("7"), "a.go")
	if len(got) != 2 {
		t.Fatalf("a.go threads = %d, want 2 (the outdated one belongs to the hub)", len(got))
	}
	root := got[0]
	if root.Note.ID != "forge:C1" || root.Note.Source != model.NoteSourceForge || root.Note.Author != "carol" ||
		root.Note.Side != model.NoteSideNew || root.Range != [2]int{10, 12} || root.Note.Range != [2]int{10, 12} ||
		root.Status != model.NoteActive || root.Note.Address.Path != "a.go" {
		t.Fatalf("root = %+v", root)
	}
	if root.Note.Summary != "rename this" || root.Note.Rationale != "it shadows the package" {
		t.Fatalf("summary/rationale = %q / %q", root.Note.Summary, root.Note.Rationale)
	}
	if !model.NoteHasTag(root.Note, model.NoteTagResolved) {
		t.Fatalf("a resolved thread carries the tag: %v", root.Note.Tags)
	}
	if len(root.Replies) != 1 || root.Replies[0].Note.ID != "forge:C2" || root.Replies[0].Note.ParentID != "forge:C1" {
		t.Fatalf("replies = %+v", root.Replies)
	}
	if left := got[1]; left.Note.Side != model.NoteSideOld || left.Range != [2]int{3, 3} {
		t.Fatalf("a LEFT-side comment must survive with its side: %+v", left)
	}
	file := svc.forgeNotesFor(prSet("7"), "b.txt")
	if len(file) != 1 || file[0].Range != [2]int{0, 0} || file[0].Note.Side != model.NoteSideNew {
		t.Fatalf("file-level = %+v", file)
	}
	if ff.commentCalls != 1 {
		t.Fatalf("reads are served from the cache: provider calls = %d", ff.commentCalls)
	}
	if got := svc.forgeNotesFor(PreviewNoteSet{Source: "feat/x", Tip: "t"}, "a.go"); got != nil {
		t.Fatal("an ordinary preview has no forge notes")
	}
}

func TestPRCommentsRefreshReportsChange(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{comments: reviewThreads()}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if changed, _ := svc.PRCommentsRefresh(ctx, 7); changed {
		t.Fatal("identical data is not a change")
	}
	ff.mu.Lock()
	ff.comments[2].Resolved = true
	ff.mu.Unlock()
	if changed, _ := svc.PRCommentsRefresh(ctx, 7); !changed {
		t.Fatal("a thread resolving is a change")
	}
	ff.mu.Lock()
	ff.commentsErr = errors.New("HTTP 502")
	ff.mu.Unlock()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err == nil {
		t.Fatal("a failed refresh reports its error")
	}
	if got := svc.forgeNotesFor(prSet("7"), "a.go"); len(got) != 2 {
		t.Fatalf("a failed refresh keeps the previous comments, got %d", len(got))
	}
}

func TestPreviewReadsIncludeForgeNotes(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{comments: reviewThreads()}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	ns, err := svc.PreviewNotesFor(ctx, prSet("7"), "a.go", Diff{})
	if err != nil || len(ns) != 2 {
		t.Fatalf("PreviewNotesFor = %d notes, err %v", len(ns), err)
	}
	byPath, total, err := svc.PreviewNoteCounts(ctx, prSet("7"))
	if err != nil || total != 3 || byPath["a.go"] != 2 || byPath["b.txt"] != 1 {
		t.Fatalf("counts = %v total %d err %v", byPath, total, err)
	}
	all, err := svc.PreviewNotesAll(ctx, prSet("7"))
	if err != nil || len(all["a.go"]) != 2 || len(all["b.txt"]) != 1 {
		t.Fatalf("PreviewNotesAll = %v err %v", all, err)
	}
}

func TestForgeNotesAreReadOnly(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{})
	ctx := context.Background()
	if err := svc.NoteEdit(ctx, "forge:C1", "x", ""); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("NoteEdit = %v", err)
	}
	if _, err := svc.NoteReply(ctx, "forge:C1", model.Note{Summary: "x"}); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("NoteReply = %v", err)
	}
	if err := svc.NoteRemove(ctx, "forge:C1"); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("NoteRemove = %v", err)
	}
}

// The diff-less read (the web handler, the CLI, MCP) must see the same forge
// threads the TUI's PreviewNotesFor does — before this it returned nothing for
// a file with no stored notes, while the counts already included them.
func TestPreviewNotesAtMergesForgeThreads(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{comments: reviewThreads()}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	ff.commentCalls = 0
	ns, err := svc.PreviewNotesAt(ctx, prSet("7"), "a.go")
	if err != nil || len(ns) != 2 || ns[0].Note.ID != "forge:C1" || len(ns[0].Replies) != 1 {
		t.Fatalf("PreviewNotesAt(a.go) = %+v, err %v", ns, err)
	}
	if file, err := svc.PreviewNotesAt(ctx, prSet("7"), "b.txt"); err != nil || len(file) != 1 {
		t.Fatalf("PreviewNotesAt(b.txt) = %d notes, err %v", len(file), err)
	}
	if ff.commentCalls != 0 {
		t.Fatalf("a read called the provider %d times", ff.commentCalls)
	}
}

func TestPRCommentsCachedIsEmptyBeforeARefresh(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{comments: reviewThreads()})
	if _, ok := svc.PRCommentsCached(7); ok {
		t.Fatal("nothing was fetched yet")
	}
	if _, err := svc.PRCommentsRefresh(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	c, ok := svc.PRCommentsCached(7)
	if !ok || len(c.Outdated) != 1 || len(c.Hub) != 1 {
		t.Fatalf("cached = %+v ok %v", c, ok)
	}
}

// The details view is served stale-first: what the last forge read left in the
// caches, with no forge call; nothing until BOTH halves were read once.
func TestPRDetailsCached(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{comments: reviewThreads(), byNum: map[int]model.PullRequest{7: {Number: 7, Title: "t", Body: "the body"}}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	if _, _, ok := svc.PRDetailsCached(7); ok {
		t.Fatal("nothing was read yet")
	}
	if _, err := svc.PullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := svc.PRDetailsCached(7); ok {
		t.Fatal("the comments were never read: half a view is not a cached view")
	}
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	ff.commentCalls = 0
	pr, c, ok := svc.PRDetailsCached(7)
	if !ok || pr.Body != "the body" || len(c.Hub) != 1 || len(c.Outdated) != 1 {
		t.Fatalf("cached = %+v %+v ok %v", pr, c, ok)
	}
	if ff.commentCalls != 0 {
		t.Fatal("a cached read called the forge")
	}
}

func TestWireNoteForgeFields(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{comments: reviewThreads()})
	if _, err := svc.PRCommentsRefresh(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	root := ToWireNote(svc.forgeNotesFor(prSet("7"), "a.go")[0])
	if !root.ReadOnly || !root.Resolved || root.FileLevel || root.Created != "2023-11-14T22:13:20Z" {
		t.Fatalf("resolved line thread = %+v", root)
	}
	if len(root.Replies) != 1 || !root.Replies[0].ReadOnly || root.Replies[0].Resolved {
		t.Fatalf("reply = %+v", root.Replies)
	}
	file := ToWireNote(svc.forgeNotesFor(prSet("7"), "b.txt")[0])
	if !file.ReadOnly || !file.FileLevel {
		t.Fatalf("file-level thread = %+v", file)
	}
	mine := ToWireNote(ResolvedNote{Note: model.Note{ID: "n1", Source: model.NoteSourceUser}})
	if mine.ReadOnly || mine.Resolved || mine.FileLevel || mine.Created != "" {
		t.Fatalf("a stored note = %+v", mine)
	}
}
