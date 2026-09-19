package domain

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/preflight"
)

// fakeForge is a scriptable forge.Provider.
type fakeForge struct {
	mu         sync.Mutex
	detectErr  error
	detectGate chan struct{} // non-nil: Detect blocks until it is closed
	detects    atomic.Int32
	open       []model.PullRequest
	byNum      map[int]model.PullRequest // PR(n); missing → forge.ErrNotFound
	prCalls    map[int]int
	comments   []model.ForgeComment
	truncated  bool
	slug, url  string
}

func (f *fakeForge) Name() string { return "fake" }
func (f *fakeForge) Detect(context.Context) error {
	f.detects.Add(1)
	if f.detectGate != nil {
		<-f.detectGate
	}
	return f.detectErr
}
func (f *fakeForge) ListOpen(context.Context) ([]model.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.PullRequest(nil), f.open...), nil
}
func (f *fakeForge) PR(_ context.Context, n int) (model.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prCalls == nil {
		f.prCalls = map[int]int{}
	}
	f.prCalls[n]++
	p, ok := f.byNum[n]
	if !ok {
		return model.PullRequest{}, forge.ErrNotFound
	}
	return p, nil
}
func (f *fakeForge) Comments(context.Context, int) ([]model.ForgeComment, bool, error) {
	return f.comments, f.truncated, nil
}
func (f *fakeForge) BaseRepo(context.Context) (string, string, error) { return f.slug, f.url, nil }
func (f *fakeForge) HeadRefspec(int) string                           { return "refs/pull/x/head" }

func newForgeSvc(t *testing.T, ff *fakeForge) *Service {
	t.Helper()
	_, svc := newRealRepo(t)
	svc.SetForgeProviders([]forge.Provider{ff})
	return svc
}

func TestForgeStatusProbesOnce(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{detectErr: errors.New("not logged in")}
	svc := newForgeSvc(t, ff)
	for range 3 {
		st := svc.ForgeStatus(context.Background())
		if st.Available() || !errors.Is(st.Err, ErrForgeUnavailable) {
			t.Fatalf("status = %+v", st)
		}
	}
	if n := ff.detects.Load(); n != 1 {
		t.Errorf("Detect ran %d times, want 1 (no re-detection)", n)
	}
	if _, err := svc.PullRequests(context.Background()); !errors.Is(err, ErrForgeUnavailable) {
		t.Errorf("PullRequests err = %v, want ErrForgeUnavailable", err)
	}
}

func TestForgeFeatureVerdictFollowsStatus(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{}
	svc := newForgeSvc(t, ff)
	verdict := func() preflight.Verdict {
		vs, err := svc.Preflight(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			if v.Feature.ID == FeatureForge {
				return v
			}
		}
		t.Fatal("no forge verdict")
		return preflight.Verdict{}
	}
	if v := verdict(); v.State == preflight.Satisfied || !v.Feature.Silent {
		t.Errorf("before the probe: %+v", v)
	}
	if n := ff.detects.Load(); n != 0 {
		t.Errorf("Preflight ran the network probe itself (%d detects)", n)
	}
	svc.ForgeStatus(context.Background())
	if v := verdict(); v.State != preflight.Satisfied {
		t.Errorf("after the probe: %+v", v)
	}
}

func pr(n int, state string, upd int) model.PullRequest {
	return model.PullRequest{Number: n, State: state, Title: "t", HeadSHA: "h", BaseSHA: "b",
		Target: "main", Updated: time.Unix(int64(upd), 0)}
}

func (f *fakeForge) setOpen(prs ...model.PullRequest) {
	f.mu.Lock()
	f.open = prs
	f.mu.Unlock()
}

func TestPullRequestsKeepsKnownClosedOnes(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{open: []model.PullRequest{pr(1, "open", 10), pr(2, "open", 20)},
		byNum: map[int]model.PullRequest{1: pr(1, "merged", 30)}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	got, err := svc.PullRequests(ctx)
	if err != nil || len(got) != 2 || got[0].Number != 2 {
		t.Fatalf("first = %+v, %v (want #2 then #1, newest first)", got, err)
	}
	ff.setOpen(pr(2, "open", 20)) // #1 got merged
	for range 2 {
		got, err = svc.PullRequests(ctx)
		if err != nil || len(got) != 2 || got[0].Number != 2 || got[1].Number != 1 || got[1].State != model.PRStateMerged {
			t.Fatalf("after merge = %+v, %v (open first, then the merged one)", got, err)
		}
	}
	ff.mu.Lock()
	calls := ff.prCalls[1]
	ff.mu.Unlock()
	if calls != 1 {
		t.Errorf("PR(1) read %d times, want 1 (a terminal state is cached)", calls)
	}
}

func TestPullRequestsListsFetchedRefsAcrossSessions(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{byNum: map[int]model.PullRequest{5: pr(5, "closed", 5)}}
	svc := newForgeSvc(t, ff) // a fresh Service = a fresh session: nothing "seen"
	ctx := context.Background()
	head, err := svc.repo.ResolveCommit(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{5, 6} { // 6 is unknown to the forge
		if err := svc.repo.UpdateRef(ctx, git.PRRef(n), head); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.PullRequests(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	states := map[int]string{got[0].Number: got[0].State, got[1].Number: got[1].State}
	if states[5] != model.PRStateClosed || states[6] != model.PRStateUnavailable {
		t.Errorf("states = %v", states)
	}
}

func TestForgetDropsSessionKnownPR(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{open: []model.PullRequest{pr(1, "open", 1)}, byNum: map[int]model.PullRequest{1: pr(1, "closed", 2)}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	if _, err := svc.PullRequests(ctx); err != nil {
		t.Fatal(err)
	}
	ff.setOpen()
	if got, _ := svc.PullRequests(ctx); len(got) != 1 {
		t.Fatalf("closed PR vanished: %+v", got)
	}
	if _, err := svc.Execute(ctx, svc.PRForgetOp(1), nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.PullRequests(ctx); len(got) != 0 {
		t.Errorf("after forget = %+v", got)
	}
}

func TestPRCommentsBuckets(t *testing.T) {
	t.Parallel()
	at := func(s int) time.Time { return time.Unix(int64(s), 0) }
	ff := &fakeForge{truncated: true, comments: []model.ForgeComment{
		{ID: "R", Kind: model.ForgeCommentReview, Created: at(30)},
		{ID: "I", Kind: model.ForgeCommentInline, Path: "a.go", Line: 3},
		{ID: "O", Kind: model.ForgeCommentInline, Path: "a.go", Line: 9, Outdated: true},
		{ID: "Z", Kind: model.ForgeCommentInline, Path: "a.go"}, // no line, not flagged: still has no position
		{ID: "F", Kind: model.ForgeCommentFile, Path: "b.go"},
		{ID: "X", Kind: model.ForgeCommentFile, Path: "b.go", Outdated: true},
		{ID: "G", Kind: model.ForgeCommentGeneral, Created: at(10)},
	}}
	c, err := newForgeSvc(t, ff).PRComments(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(cs []model.ForgeComment) (s string) {
		for _, c := range cs {
			s += c.ID
		}
		return
	}
	if ids(c.Inline) != "IF" || ids(c.Outdated) != "OZX" || ids(c.Hub) != "GR" || !c.Truncated {
		t.Errorf("inline=%s outdated=%s hub=%s truncated=%v", ids(c.Inline), ids(c.Outdated), ids(c.Hub), c.Truncated)
	}
}

func TestPRFetchOpPrefersAMatchingRemote(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{slug: "github.com/homeend/gigagit", url: "git@github.com:homeend/gigagit.git",
		byNum: map[int]model.PullRequest{7: pr(7, "open", 1)}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	op, err := svc.PRFetchOp(ctx, 7)
	if err != nil || op.Remote != ff.url || op.Refspec != "refs/pull/x/head" || op.HeadSHA != "h" || op.Number != 7 {
		t.Fatalf("no remotes: op = %+v, %v (want the fallback URL)", op, err)
	}
	addRemote := func(name, url string) {
		t.Helper()
		if _, err := svc.repo.Runner.Run(ctx, "git remote add", gitcmd.New("remote").Arg("add", name, url).ToArgv()); err != nil {
			t.Fatal(err)
		}
	}
	addRemote("origin", "https://github.com/someone/fork.git")
	addRemote("upstream", "https://github.com/HomeEnd/gigagit.git") // same repo, other spelling
	op, err = svc.PRFetchOp(ctx, 7)
	if err != nil || op.Remote != "upstream" {
		t.Fatalf("op = %+v, %v (want remote name upstream)", op, err)
	}
	if _, err := svc.PRFetchOp(ctx, 99); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("PRFetchOp(99) err = %v, want ErrNotFound", err)
	}
}

func TestPRPair(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{})
	ctx := context.Background()
	head, err := svc.repo.ResolveCommit(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if p := svc.PRPair(ctx, model.PullRequest{Number: 7, State: "open", Target: "main", BaseSHA: head}); p.Base != "main" || p.Head != "refs/gg/pr/7" {
		t.Errorf("open = %+v", p)
	}
	if p := svc.PRPair(ctx, model.PullRequest{Number: 7, State: "merged", Target: "main", BaseSHA: head}); p.Base != head {
		t.Errorf("merged = %+v (want the recorded base sha)", p)
	}
	if p := svc.PRPair(ctx, model.PullRequest{Number: 7, State: "open", Target: "no-such-branch", BaseSHA: head}); p.Base != head {
		t.Errorf("missing target = %+v (want the base sha fallback)", p)
	}
	// Target only exists as a remote-tracking branch and the base sha is not here.
	if _, err := svc.repo.Runner.Run(ctx, "git remote add", gitcmd.New("remote").Arg("add", "origin", "https://example.test/r.git").ToArgv()); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.UpdateRef(ctx, "refs/remotes/origin/dev", head); err != nil {
		t.Fatal(err)
	}
	missing := "0123456789012345678901234567890123456789"
	if p := svc.PRPair(ctx, model.PullRequest{Number: 7, State: "open", Target: "dev", BaseSHA: missing}); p.Base != "origin/dev" {
		t.Errorf("remote-tracking fallback = %+v", p)
	}
}

// The probe is a network call that may hang for its whole timeout. Preflight —
// which notices, FeatureEnabled and the web gate all call — must answer
// "unprobed" straight away instead of queueing behind it.
func TestPreflightDoesNotWaitForAnInFlightProbe(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{detectGate: make(chan struct{})}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	statuses := make(chan ForgeStatus, 2)
	for range 2 { // two concurrent first callers share ONE probe
		go func() { statuses <- svc.ForgeStatus(ctx) }()
	}
	deadline := time.Now().Add(5 * time.Second)
	for ff.detects.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the probe never started")
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan []preflight.Verdict, 1)
	go func() {
		vs, _ := svc.Preflight(ctx)
		done <- vs
	}()
	select {
	case vs := <-done:
		for _, v := range vs {
			if v.Feature.ID == FeatureForge && v.State == preflight.Satisfied {
				t.Error("forge verdict Satisfied while the probe is still in flight")
			}
		}
	case <-time.After(3 * time.Second):
		close(ff.detectGate)
		t.Fatal("Preflight blocked behind the in-flight forge probe")
	}
	close(ff.detectGate)
	for range 2 {
		if st := <-statuses; !st.Available() {
			t.Errorf("status = %+v", st)
		}
	}
	if n := ff.detects.Load(); n != 1 {
		t.Errorf("Detect ran %d times, want 1", n)
	}
}

// A cancelled caller is not a verdict about gh: the next caller probes again.
func TestForgeStatusCancelledProbeIsNotCached(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{detectErr: context.Canceled}
	svc := newForgeSvc(t, ff)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if st := svc.ForgeStatus(ctx); st.Available() {
		t.Fatalf("status = %+v", st)
	}
	ff.detectErr = nil
	if st := svc.ForgeStatus(context.Background()); !st.Available() {
		t.Errorf("after a cancelled probe the next call must probe again: %+v", st)
	}
	if n := ff.detects.Load(); n != 2 {
		t.Errorf("Detect ran %d times, want 2", n)
	}
}
