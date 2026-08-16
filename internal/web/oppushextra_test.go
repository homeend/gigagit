package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// newCloneWithTipTag builds a clone whose current branch tip carries a local
// tag the origin has never seen, plus an older tag further back in history
// that must stay out of every tip-tag decision. Returns (clone, origin).
func newCloneWithTipTag(t *testing.T) (string, string) {
	t.Helper()
	clone, origin := newNarrowClone(t)
	gitRun(t, clone, "tag", "old-tag", "HEAD~1") // history, not the tip
	gitRun(t, clone, "tag", "v1.0")              // the tip
	return clone, origin
}

// The pre-push check finds the local-only tip tag, and only that one.
func TestPushCheckFindsLocalOnlyTipTag(t *testing.T) {
	clone, _ := newCloneWithTipTag(t)
	ts := serve(t, New(domain.Open(clone)))

	var got pushCheckResp
	if code := getJSON(t, ts, "/api/push-check", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !got.Checked {
		t.Fatalf("checked = false, want the lookup to have finished: %+v", got)
	}
	if got.Branch != "feat" {
		t.Errorf("branch = %q, want feat", got.Branch)
	}
	if len(got.Unpushed) != 1 || got.Unpushed[0] != "v1.0" {
		t.Errorf("unpushed = %v, want [v1.0] — a tag further back is not this prompt's business", got.Unpushed)
	}
}

// A tag the remote already has is not offered.
func TestPushCheckSkipsTagsTheRemoteHas(t *testing.T) {
	clone, _ := newCloneWithTipTag(t)
	gitRun(t, clone, "push", "origin", "v1.0")
	ts := serve(t, New(domain.Open(clone)))

	var got pushCheckResp
	getJSON(t, ts, "/api/push-check", &got)
	if !got.Checked || len(got.Unpushed) != 0 {
		t.Errorf("got %+v, want checked with nothing to offer", got)
	}
}

// A tip with no tags answers without touching the network at all — the TUI's
// fast path, and the reason an ordinary push costs nothing extra.
func TestPushCheckNoTipTags(t *testing.T) {
	clone, _ := newNarrowClone(t)
	ts := serve(t, New(domain.Open(clone)))

	var got pushCheckResp
	getJSON(t, ts, "/api/push-check", &got)
	if !got.Checked || len(got.TipTags) != 0 || len(got.Unpushed) != 0 {
		t.Errorf("got %+v, want a checked, empty answer", got)
	}
}

// THE budget test: a lookup that runs out of time answers checked=false with
// nothing to offer, so the client pushes normally. An unreachable remote must
// never turn a push into a hang.
func TestPushCheckBudgetExpiryOffersNothing(t *testing.T) {
	clone, _ := newCloneWithTipTag(t)
	old := pushCheckBudget
	pushCheckBudget = time.Nanosecond
	t.Cleanup(func() { pushCheckBudget = old })
	ts := serve(t, New(domain.Open(clone)))

	var got pushCheckResp
	if code := getJSON(t, ts, "/api/push-check", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a timeout is not an error here", code)
	}
	if got.Checked {
		t.Errorf("checked = true, want false on an expired budget: %+v", got)
	}
	if len(got.Unpushed) != 0 {
		t.Errorf("unpushed = %v, want nothing offered when the check did not finish", got.Unpushed)
	}
}

// A named branch is checked instead of the current one — with ITS tip's tags.
// old-tag sits on feat~1, which is main's tip, so it is main's tip tag and not
// feat's: the same tag answers differently per branch, which is the whole
// point of anchoring the check to a tip rather than to history.
func TestPushCheckNamedBranch(t *testing.T) {
	clone, _ := newCloneWithTipTag(t)
	ts := serve(t, New(domain.Open(clone)))

	var got pushCheckResp
	getJSON(t, ts, "/api/push-check?branch=main", &got)
	if got.Branch != "main" || len(got.TipTags) != 1 || got.TipTags[0] != "old-tag" {
		t.Errorf("got %+v, want main with tip tag old-tag", got)
	}
}

func TestPushCheckRejectsOptionLikeBranch(t *testing.T) {
	clone, _ := newNarrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	if code := getJSON(t, ts, "/api/push-check?branch=--upload-pack=evil", nil); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}

// The op pushes exactly the tip tags — the one further back stays local.
func TestPushTipTagsPushesOnlyTheTip(t *testing.T) {
	clone, origin := newCloneWithTipTag(t)
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"push-tip-tags"}`), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	remote := gitRun(t, origin, "tag", "--list")
	if !hasLine(remote, "v1.0") {
		t.Errorf("origin tags = %q, want v1.0 pushed", remote)
	}
	if hasLine(remote, "old-tag") {
		t.Errorf("origin tags = %q, want the non-tip tag left alone", remote)
	}
}

// Nothing on the tip is a refusal with its own status, not a run that does
// nothing.
func TestPushTipTagsRefusesWithoutTipTags(t *testing.T) {
	clone, _ := newNarrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	if code := postJSON(t, ts, "/api/op", `{"op":"push-tip-tags"}`,
		"application/json", "", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("code = %d, want 422", code)
	}
}

func TestPushTipTagsRejectsOptionLikeBranch(t *testing.T) {
	clone, _ := newCloneWithTipTag(t)
	ts := serve(t, New(domain.Open(clone)))
	if code := postJSON(t, ts, "/api/op", `{"op":"push-tip-tags","branch":"--exec=evil"}`,
		"application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}
