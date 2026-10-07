package domain

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// sendRepo: main holds big.go (30 lines) and other.go; the PR (#7) changes
// big.go line 5 and line 25 (two hunks: 2–8 and 22–28 at -U3). Returns the
// service (fake forge + note store), the fake forge and the PR head.
func sendRepo(t *testing.T) (*Service, *fakeForge, string) {
	t.Helper()
	dir, _ := newRealRepo(t)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	big := strings.Join(lines, "\n") + "\n"
	commitFile(t, dir, "big.go", big, "big")
	commitFile(t, dir, "other.go", "package other\n", "other")
	runGitIn(t, dir, "checkout", "-q", "-b", "feat")
	lines[4], lines[24] = "line 5 changed", "line 25 changed"
	commitFile(t, dir, "big.go", strings.Join(lines, "\n")+"\n", "change")
	head := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "update-ref", git.PRRef(7), head)
	runGitIn(t, dir, "checkout", "-q", "main")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	ff := &fakeForge{slug: "github.com/o/r",
		byNum: map[int]model.PullRequest{7: {Number: 7, State: "open", Target: "main", HeadSHA: head, NodeID: "PR_7"}}}
	svc.SetForgeProviders([]forge.Provider{ff})
	return svc, ff, head
}

func addPRNote(t *testing.T, svc *Service, commit, path string, line int, sum string) string {
	t.Helper()
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: sum,
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: path},
		Side:    model.NoteSideNew, Range: [2]int{line, line}})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestPlanSendPlacesEachNote(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	inHunk := addPRNote(t, svc, head, "big.go", 5, "in the hunk")
	between := addPRNote(t, svc, head, "big.go", 15, "between the hunks")
	untouched := addPRNote(t, svc, head, "other.go", 1, "file not in the PR")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{inHunk, between, untouched}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != engine.SendReview || p.Verdict || p.Target != "o/r #7" || p.PRID != "PR_7" || p.Head != head {
		t.Fatalf("plan = %+v", p)
	}
	if len(p.Items) != 2 || len(p.Skipped) != 1 {
		t.Fatalf("items %+v skipped %+v", p.Items, p.Skipped)
	}
	if th := p.Items[0].Thread; p.Items[0].Key != inHunk || th.Path != "big.go" || th.Line != 5 || th.Side != model.NoteSideNew {
		t.Errorf("line thread = %+v", p.Items[0])
	}
	if th := p.Items[1].Thread; th.Line != 0 || !strings.HasPrefix(th.Body, "> Line 15:") {
		t.Errorf("file-level thread = %+v", th)
	}
	if p.Skipped[0].Reason != "not in this PR" {
		t.Errorf("skip = %+v", p.Skipped[0])
	}
}

func TestPlanSendIgnoresTheUsersDiffContext(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	top, _ := svc.TopLevel(context.Background())
	runGitIn(t, strings.TrimSpace(top), "config", "diff.context", "10") // would merge the two hunks
	between := addPRNote(t, svc, head, "big.go", 15, "between")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{between}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Items[0].Thread.Line != 0 {
		t.Fatalf("line 15 is outside GitHub's 3-line hunks: %+v", p.Items[0].Thread)
	}
}

func TestPlanSendRefusesAMovedHead(t *testing.T) {
	t.Parallel()
	svc, ff, _ := sendRepo(t)
	ff.mu.Lock()
	pr := ff.byNum[7]
	pr.HeadSHA = strings.Repeat("e", 40)
	ff.byNum[7] = pr
	ff.mu.Unlock()
	if _, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Mine: true}); err == nil || !strings.Contains(err.Error(), "new commits") {
		t.Fatalf("err = %v", err)
	}
}

func TestPRSendOpEndToEndDeletesTheSentNote(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	id := addPRNote(t, svc, head, "big.go", 5, "in the hunk")
	keep := addPRNote(t, svc, head, "other.go", 1, "not in the PR")
	ctx := context.Background()
	op, err := svc.PRSendOp(ctx, PRSendRequest{PR: 7, Notes: []string{id, keep}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Execute(ctx, op, nil, engine.MapDecider{engine.DecisionSendForge: engine.OptSend}); err != nil {
		t.Fatal(err)
	}
	if got := ff.writeLog(); !strings.Contains(got, "StartReview") || !strings.Contains(got, "SubmitReview COMMENT") {
		t.Fatalf("writes = %s", got)
	}
	if _, ok := noteByID(t, svc, id); ok {
		t.Error("the sent note is still local")
	}
	if n, ok := noteByID(t, svc, keep); !ok || n.Send != nil {
		t.Errorf("the skipped note must stay local and unstamped: %+v %v", n.Send, ok)
	}
}

func TestPRSendOpNeedsAWritableForge(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	svc.SetForgeProviders([]forge.Provider{readOnlyForge{&fakeForge{}}})
	if _, err := svc.PRSendOp(context.Background(), PRSendRequest{PR: 7, Mine: true}); err == nil {
		t.Fatal("a provider with no Writer cannot send")
	}
}

func TestPlanSendWholeReviewKeysEveryRemark(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	doc := `{"version":1,"summary":"looks fine","files":[{"path":"big.go","annotations":[
 {"newRange":[5,5],"summary":"check this"},{"newRange":[25,25],"summary":"and this"}]}]}`
	rid, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: "feat"},
		Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Review: rid})
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != rid || !p.Verdict || len(p.Items) != 2 || !strings.HasPrefix(p.Items[0].Key, "remark:"+rid+":") ||
		!strings.Contains(p.Body, "looks fine") || !strings.Contains(p.Body, "— claude via gg") {
		t.Fatalf("plan = %+v", p)
	}
}

func TestPlanSendSkipsAStaleNoteAndLeavesItLocal(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	stale := addPRNote(t, svc, head, "big.go", 5, "about the old text")
	fresh := addPRNote(t, svc, head, "big.go", 25, "still here")
	// Its anchored text is gone from the PR head (the file is not): stale.
	if err := svc.notesStore(ctx).Edit(stale, func(n *model.Note) error { n.ContextHash = "gone"; return nil }); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Notes: []string{stale, fresh}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Key != fresh {
		t.Fatalf("items = %+v", p.Items)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].Reason != "its lines changed" || !strings.HasPrefix(p.Skipped[0].Label, "big.go:5 ") {
		t.Fatalf("skipped = %+v", p.Skipped)
	}
	if n, ok := noteByID(t, svc, stale); !ok || n.Send != nil {
		t.Fatalf("the stale note must stay local and unstamped: %+v %v", n.Send, ok)
	}
}

// R4: a whole-review send whose remarks did not ALL reach GitHub keeps the
// review local — and must remember its summary did, or a re-send posts it
// again.
func TestPartlySentReviewKeepsItsSummaryOnGitHub(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	doc := `{"version":1,"summary":"looks fine","files":[{"path":"big.go","annotations":[
 {"newRange":[5,5],"summary":"check this"},{"newRange":[25,25],"summary":"and this"}]}]}`
	rid, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: "feat"},
		Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, rid)
	fps := r.remarkFPs()
	at := settleT0.Add(-time.Minute)
	if err := svc.notesStore(ctx).Edit(rid, func(n *model.Note) error {
		n.Send = &model.NoteSend{PR: 7, Review: "PRR_done", At: at}
		n.RemarkSends = []model.RemarkSend{
			{RemarkFP: fps[0], Send: model.NoteSend{PR: 7, Review: "PRR_done", Thread: "PRRT_x", At: at}},
			{RemarkFP: fps[1], Send: model.NoteSend{PR: 7, Review: "PRR_done", Thread: "PRRT_y", At: at}},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	// The review was submitted; only the first remark's thread survived
	// (the second was deleted on GitHub before this read).
	ff.mu.Lock()
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_x", Kind: model.ForgeCommentInline, Path: "big.go",
		Line: 5, Body: "check this\n\n" + forge.SendMarker(RemarkKey(rid, fps[0])), ThreadID: "PRRT_x", ReviewID: "PRR_done"})
	ff.mu.Unlock()
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Review: rid})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Key != RemarkKey(rid, fps[1]) {
		t.Fatalf("items = %+v (only the remark GitHub lacks)", p.Items)
	}
	if strings.TrimSpace(p.Body) != "" || !p.Verdict {
		t.Fatalf("body = %q verdict = %v: the summary is already on GitHub", p.Body, p.Verdict)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].Reason != "already on GitHub" {
		t.Fatalf("skipped = %+v", p.Skipped)
	}
}
