package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// fakeWriter records every call as "Op arg…" and fails the ops in fail.
type fakeWriter struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]error
	n     int
}

func (w *fakeWriter) rec(op string, args ...any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	parts := []string{op} // fmt.Sprint puts no space between string operands
	for _, a := range args {
		parts = append(parts, fmt.Sprint(a))
	}
	w.calls = append(w.calls, strings.Join(parts, " "))
	if err := w.fail[op]; err != nil {
		delete(w.fail, op) // fail once
		return err
	}
	return nil
}
func (w *fakeWriter) StartReview(_ context.Context, pr, commit string) (string, error) {
	return "R1", w.rec("StartReview", pr, commit)
}
func (w *fakeWriter) AddThread(_ context.Context, review string, t forge.Thread) (forge.ThreadRef, error) {
	w.n++
	id := fmt.Sprintf("T%d", w.n)
	return forge.ThreadRef{ID: id, CommentID: "C" + id, URL: "u/" + id}, w.rec("AddThread", review, t.Path, t.Line)
}
func (w *fakeWriter) Reply(_ context.Context, review, thread, body string) (forge.CommentRef, error) {
	return forge.CommentRef{ID: "RC" + thread}, w.rec("Reply", review, thread, body)
}
func (w *fakeWriter) SubmitReview(_ context.Context, review string, ev forge.Event, body string) error {
	return w.rec("SubmitReview", review, ev, body)
}
func (w *fakeWriter) DeletePendingReview(_ context.Context, review string) error {
	return w.rec("DeletePendingReview", review)
}
func (w *fakeWriter) Resolve(_ context.Context, th string) error   { return w.rec("Resolve", th) }
func (w *fakeWriter) Unresolve(_ context.Context, th string) error { return w.rec("Unresolve", th) }

type fakeLedger struct {
	stamps       map[string]model.NoteSend
	failed       map[string]string
	failedReview map[string]string // the review id a failure kept ("" = none)
	settled      int
}

func newLedger() *fakeLedger {
	return &fakeLedger{stamps: map[string]model.NoteSend{}, failed: map[string]string{}, failedReview: map[string]string{}}
}
func (l *fakeLedger) Stamp(_ context.Context, key string, s model.NoteSend) error {
	l.stamps[key] = s
	return nil
}
func (l *fakeLedger) Fail(_ context.Context, keys []string, review string, err error) {
	for _, k := range keys {
		delete(l.stamps, k)
		l.failed[k] = err.Error()
		l.failedReview[k] = review
	}
}
func (l *fakeLedger) Settle(context.Context) error { l.settled++; return nil }

func plan2() SendPlan {
	return SendPlan{Target: "o/r #7", PR: 7, PRID: "PR_7", Head: "h1", Mode: SendReview, Verdict: true, Body: "summary",
		Items: []SendItem{
			{Key: "n1", Label: "a.go:3 x", Kind: SendThread, Thread: forge.Thread{Path: "a.go", Line: 3, Body: "x"},
				Replies: []SendReplyBody{{Key: "n1r", Body: "and y"}}, Resolve: true},
			{Key: "n2", Label: "b.go (file) z", Kind: SendThread, Thread: forge.Thread{Path: "b.go", Body: "z"}},
		},
		Skipped: []SendSkip{{Label: "c.go:1 q", Reason: "not in this PR"}}}
}

func runSend(t *testing.T, p SendPlan, w *fakeWriter, l *fakeLedger, answer string) (Result, error, []DecisionRequest) {
	t.Helper()
	var asked []DecisionRequest
	dec := DeciderFunc(func(_ context.Context, r DecisionRequest) (DecisionResponse, error) {
		asked = append(asked, r)
		return DecisionResponse{Option: answer}, nil
	})
	op := SendToForge{Plan: p, Writer: w, Ledger: l,
		Now: func() time.Time { return time.Unix(100, 0) }}
	res, err := op.Run(context.Background(), OpDeps{Decider: dec})
	return res, err, asked
}

func TestSendReviewWritesInOrderAndSettles(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	res, err, asked := runSend(t, plan2(), w, l, OptApprove)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"StartReview PR_7 h1", "AddThread R1 a.go 3", "Reply R1 T1 and y", "AddThread R1 b.go 0",
		"SubmitReview R1 APPROVE summary", "Resolve T1"}
	if !slices.Equal(w.calls, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(w.calls, "\n"), strings.Join(want, "\n"))
	}
	if len(asked) != 1 || asked[0].ID != DecisionSendForge ||
		!slices.Equal(asked[0].Options, []string{OptComment, OptApprove, OptRequestChanges, "abort"}) {
		t.Fatalf("asked = %+v", asked)
	}
	for _, s := range []string{"o/r #7", "a.go:3 x", "b.go (file) z", "c.go:1 q", "not in this PR", "summary"} {
		if !strings.Contains(asked[0].Prompt, s) {
			t.Errorf("confirm lacks %q:\n%s", s, asked[0].Prompt)
		}
	}
	if l.stamps["n1"].Thread != "T1" || l.stamps["n1"].Review != "R1" || l.stamps["n1r"].Comment != "RCT1" || l.stamps["n2"].Thread != "T2" {
		t.Errorf("stamps = %+v", l.stamps)
	}
	if l.settled != 1 || !res.Changed {
		t.Errorf("settled %d, res %+v", l.settled, res)
	}
}

func TestSendReviewOnOwnPROffersNoVerdicts(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.OwnPR = true
	_, _, asked := runSend(t, p, &fakeWriter{}, newLedger(), "abort")
	if !slices.Equal(asked[0].Options, []string{OptComment, "abort"}) {
		t.Fatalf("own PR options = %v", asked[0].Options)
	}
}

func TestSendAbortWritesNothing(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	res, err, _ := runSend(t, plan2(), w, l, "abort")
	if err != nil || res.Changed || len(w.calls) != 0 || len(l.stamps) != 0 || !strings.HasPrefix(res.Summary, "aborted") {
		t.Fatalf("res %+v err %v calls %v stamps %v", res, err, w.calls, l.stamps)
	}
}

func TestSendRefusesAnAnswerNotOffered(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.OwnPR = true
	w := &fakeWriter{}
	if _, err, _ := runSend(t, p, w, newLedger(), OptApprove); err == nil || len(w.calls) != 0 {
		t.Fatalf("approve on own PR: err %v calls %v", err, w.calls)
	}
}

func TestSendFailureBeforeSubmitDeletesThePendingReview(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{fail: map[string]error{"AddThread": nil, "SubmitReview": errors.New("HTTP 502")}}
	l := newLedger()
	_, err, _ := runSend(t, plan2(), w, l, OptComment)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("err = %v", err)
	}
	if w.calls[len(w.calls)-1] != "DeletePendingReview R1" {
		t.Fatalf("last call = %q, want the pending review deleted", w.calls[len(w.calls)-1])
	}
	for _, k := range []string{"n1", "n1r", "n2"} {
		if l.failed[k] != "SubmitReview: HTTP 502" && !strings.Contains(l.failed[k], "HTTP 502") {
			t.Errorf("%s failed = %q", k, l.failed[k])
		}
	}
	if len(l.stamps) != 0 || l.settled != 0 {
		t.Errorf("stamps %v settled %d", l.stamps, l.settled)
	}
}

func TestSendJoiningThePendingReviewNeverDeletesIt(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.Pending = "MINE"
	w := &fakeWriter{fail: map[string]error{"SubmitReview": errors.New("HTTP 502")}}
	l := newLedger()
	_, err, asked := runSend(t, p, w, l, OptSubmitWithPending)
	if !slices.Equal(asked[0].Options, []string{OptSubmitWithPending, "abort"}) {
		t.Fatalf("pending options = %v", asked[0].Options)
	}
	if err == nil {
		t.Fatal("want the submit error")
	}
	for _, c := range w.calls {
		if strings.HasPrefix(c, "StartReview") || strings.HasPrefix(c, "DeletePendingReview") {
			t.Fatalf("joined review: %q must not run (calls %v)", c, w.calls)
		}
	}
	if l.stamps["n1"].Review != "MINE" || len(l.failed) != 0 {
		t.Fatalf("joined failure keeps the stamps for --finish: stamps %v failed %v", l.stamps, l.failed)
	}
}

func TestSendRetriesABlankBody(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.Verdict, p.Body = false, ""
	p.Items = p.Items[:1]
	w := &fakeWriter{fail: map[string]error{"SubmitReview": errors.New("SubmitReview: Body can't be blank")}}
	if _, err, _ := runSend(t, p, w, newLedger(), OptSend); err != nil {
		t.Fatal(err)
	}
	subs := slices.DeleteFunc(slices.Clone(w.calls), func(c string) bool { return !strings.HasPrefix(c, "SubmitReview") })
	if !slices.Equal(subs, []string{"SubmitReview R1 COMMENT ", "SubmitReview R1 COMMENT 1 comment"}) {
		t.Fatalf("submits = %q", subs)
	}
}

func TestSendActionsGoOneByOne(t *testing.T) {
	t.Parallel()
	p := SendPlan{Target: "o/r #7", PR: 7, Mode: SendActions, Items: []SendItem{
		{Key: "d1", Label: "reply 1", Kind: SendReply, ThreadID: "TA", Body: "one"},
		{Key: "d2", Label: "reply 2", Kind: SendReply, ThreadID: "TB", Body: "two"},
		{Label: "resolve TC", Kind: SendResolve, ThreadID: "TC"},
	}}
	w := &fakeWriter{fail: map[string]error{"Reply": errors.New("HTTP 404")}}
	l := newLedger()
	res, err, asked := runSend(t, p, w, l, OptSend)
	if err == nil || !strings.Contains(err.Error(), "1 of 3") {
		t.Fatalf("err = %v (a partial failure reports how many failed)", err)
	}
	if !slices.Equal(asked[0].Options, []string{OptSend, "abort"}) {
		t.Fatalf("options = %v", asked[0].Options)
	}
	if l.failed["d1"] == "" || l.stamps["d2"].Comment != "RCTB" || !slices.Contains(w.calls, "Resolve TC") || l.settled != 1 {
		t.Fatalf("failed %v stamps %v calls %v settled %d res %+v", l.failed, l.stamps, w.calls, l.settled, res)
	}
}

func TestResolveOnlyNeedsNoConfirm(t *testing.T) {
	t.Parallel()
	p := SendPlan{Target: "o/r #7", PR: 7, Mode: SendActions, Items: []SendItem{{Label: "resolve", Kind: SendResolve, ThreadID: "T"}}}
	w := &fakeWriter{}
	_, err, asked := runSend(t, p, w, newLedger(), "never")
	if err != nil || len(asked) != 0 || !slices.Equal(w.calls, []string{"Resolve T"}) {
		t.Fatalf("err %v asked %v calls %v", err, asked, w.calls)
	}
}

func TestFinishAndDiscard(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	if _, err, _ := runSend(t, SendPlan{Target: "o/r #7", PR: 7, Mode: SendFinish, Pending: "P"}, w, l, OptSend); err != nil {
		t.Fatal(err)
	}
	if _, err, _ := runSend(t, SendPlan{Target: "o/r #7", PR: 7, Mode: SendDiscard, Pending: "P"}, w, l, OptDiscard); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.calls, []string{"SubmitReview P COMMENT ", "DeletePendingReview P"}) || l.settled != 2 {
		t.Fatalf("calls %v settled %d", w.calls, l.settled)
	}
}

func TestSendWithNothingToSendIsAnError(t *testing.T) {
	t.Parallel()
	p := SendPlan{Target: "o/r #7", PR: 7, Mode: SendReview, Skipped: []SendSkip{{Label: "x", Reason: "not in this PR"}}}
	if _, err, asked := runSend(t, p, &fakeWriter{}, newLedger(), OptSend); err == nil || len(asked) != 0 {
		t.Fatalf("err %v asked %d", err, len(asked))
	}
}

// Final review I1: a pending review gg could not delete may still hold its
// threads — the failure keeps its id so --finish/--discard can reach it.
func TestSendFailureWhenTheDeleteFailsKeepsTheReview(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{fail: map[string]error{"AddThread": errors.New("network down"), "DeletePendingReview": errors.New("network down")}}
	l := newLedger()
	if _, err, _ := runSend(t, plan2(), w, l, OptComment); err == nil {
		t.Fatal("want the error")
	}
	if l.failedReview["n1"] != "R1" || l.failed["n1"] == "" {
		t.Fatalf("failed %v review %v", l.failed, l.failedReview)
	}
}

func TestSendFailureAfterADeleteKeepsNoReview(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{fail: map[string]error{"SubmitReview": errors.New("HTTP 502")}}
	l := newLedger()
	_, _, _ = runSend(t, plan2(), w, l, OptComment)
	if l.failedReview["n1"] != "" {
		t.Fatalf("the pending review was deleted: no id may stay (%v)", l.failedReview)
	}
}

// Final review I2: stamps of a JOINED review say so — that review is the
// user's own browser draft, which --discard must never delete.
func TestSendJoinedStampsAreMarked(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.Pending = "MINE"
	l := newLedger()
	if _, err, _ := runSend(t, p, &fakeWriter{}, l, OptSubmitWithPending); err != nil {
		t.Fatal(err)
	}
	if s := l.stamps["n1"]; !s.Joined || s.Review != "MINE" {
		t.Fatalf("joined stamp = %+v", s)
	}
}

// Final review (Minor 11, re-graded): --finish retries a blank body too.
func TestFinishRetriesABlankBody(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{fail: map[string]error{"SubmitReview": errors.New("SubmitReview: Body can't be blank")}}
	p := SendPlan{Target: "o/r #7", PR: 7, Mode: SendFinish, Pending: "P",
		Items: []SendItem{{Key: "a", Label: "a"}, {Key: "b", Label: "b"}}}
	if _, err, _ := runSend(t, p, w, newLedger(), OptSend); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.calls, []string{"SubmitReview P COMMENT ", "SubmitReview P COMMENT 2 comments"}) {
		t.Fatalf("calls = %q", w.calls)
	}
}

func TestSendPlanBodyTextDropsTheMarker(t *testing.T) {
	t.Parallel()
	p := SendPlan{Body: "fine\n\n" + forge.SendMarker("n1")}
	if got := p.BodyText(); got != "fine" {
		t.Fatalf("BodyText = %q", got)
	}
}

func planWithThen() SendPlan {
	p := plan2()
	p.Then = &SendPlan{Target: p.Target, PR: p.PR, Mode: SendActions, Items: []SendItem{
		{Key: "d1", Label: "reply: addressed", Kind: SendReply, ThreadID: "T9", Body: "addressed"},
	}}
	return p
}

// A Then plan runs after the review's writes, inside the review's confirm.
func TestSendReviewThenRepliesUnderOneConfirm(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	res, err, asked := runSend(t, planWithThen(), w, l, OptComment)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 {
		t.Fatalf("asked %d times, want one confirm", len(asked))
	}
	if !strings.Contains(asked[0].Prompt, "then, as replies:") || !strings.Contains(asked[0].Prompt, "reply: addressed") {
		t.Fatalf("the confirm must list the replies:\n%s", asked[0].Prompt)
	}
	last := w.calls[len(w.calls)-1]
	if !strings.HasPrefix(last, "Reply  T9 addressed") && !strings.Contains(last, "T9 addressed") {
		t.Fatalf("the reply must be the last write: %v", w.calls)
	}
	if !strings.Contains(res.Summary, "sent 2 comments") || !strings.Contains(res.Summary, "1 repl") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if _, ok := l.stamps["d1"]; !ok {
		t.Fatal("the reply must be stamped")
	}
}

func TestSendReviewAbortRunsNoThen(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	if _, err, _ := runSend(t, planWithThen(), w, l, "abort"); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 0 || len(l.stamps) != 0 {
		t.Fatalf("abort wrote %v / stamped %v", w.calls, l.stamps)
	}
}

func TestSendReviewFailureRunsNoThen(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{fail: map[string]error{"SubmitReview": errors.New("boom")}}, newLedger()
	if _, err, _ := runSend(t, planWithThen(), w, l, OptComment); err == nil {
		t.Fatal("want the submit error")
	}
	for _, c := range w.calls {
		if strings.HasPrefix(c, "Reply  T9") || strings.Contains(c, "T9 addressed") {
			t.Fatalf("a failed review must not post its replies: %v", w.calls)
		}
	}
	if _, stamped := l.stamps["d1"]; stamped {
		t.Fatal("a reply of a failed review must not be stamped")
	}
}

func TestSendReviewThenFailureKeepsTheReview(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{fail: map[string]error{"Reply": errors.New("502")}}, newLedger()
	p := planWithThen()
	p.Items[0].Replies = nil // the fake fails the FIRST Reply: make it the Then one
	res, err, _ := runSend(t, p, w, l, OptComment)
	if err == nil || !strings.Contains(err.Error(), "the review was posted") || !strings.Contains(err.Error(), "1 of 1 failed") {
		t.Fatalf("err = %v", err)
	}
	if !res.Changed {
		t.Fatal("the review went: Changed must be true")
	}
	if l.failed["d1"] == "" {
		t.Fatal("the failed reply must be marked failed")
	}
}
