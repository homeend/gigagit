package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The TUI's P asks before pushing a branch whose TIP carries tags the remote
// does not have. The browser pushed silently, leaving the tags behind. These
// cover the check the client asks first, and the chained tag push.

// A tip tag missing upstream is offered.
func TestPushTagCheckOffersUnpushedTipTags(t *testing.T) {
	_, dir := cloneWithOrigin(t)
	localCommit(t, dir, "f.txt", "work\n", "work") // so HEAD~1 exists
	gitRun(t, dir, "tag", "v1")                    // on the tip, never pushed
	gitRun(t, dir, "tag", "old", "HEAD~1")         // not a tip tag
	ts := serve(t, New(domain.Open(dir)))

	var got struct {
		Tags []string `json:"tags"`
	}
	if code := getJSON(t, ts, "/api/push-tag-check", &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "v1" {
		t.Errorf("tags = %v, want [v1] — only the TIP's unpushed tags", got.Tags)
	}
}

// A tip tag the remote already has is not offered, and neither is a tag that
// sits further back in history.
func TestPushTagCheckSkipsPushedAndNonTipTags(t *testing.T) {
	_, dir := cloneWithOrigin(t)
	localCommit(t, dir, "f.txt", "work\n", "work")
	gitRun(t, dir, "tag", "v1")
	gitRun(t, dir, "push", "origin", "v1") // already upstream
	gitRun(t, dir, "tag", "old", "HEAD~1") // not the tip
	ts := serve(t, New(domain.Open(dir)))

	var got struct {
		Tags []string `json:"tags"`
	}
	getJSON(t, ts, "/api/push-tag-check", &got)
	if len(got.Tags) != 0 {
		t.Errorf("tags = %v, want none", got.Tags)
	}
}

// Answering "Push branch + tags" pushes the branch and then the tags, in one
// run — and the tag really lands on the remote.
func TestPushWithTipTagsPushesBoth(t *testing.T) {
	remote, dir := cloneWithOrigin(t)
	localCommit(t, dir, "f.txt", "more\n", "more work")
	gitRun(t, dir, "tag", "v2")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"push","tags":["v2"]}`), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, remote, "tag", "-l", "v2"); out != "v2" {
		t.Errorf("remote tags = %q, want v2 pushed with the branch", out)
	}
}

// A name off the wire is verified against what is actually at the tip: a tag
// that is not there is dropped rather than pushed.
func TestPushTagsIgnoresTagsNotAtTheTip(t *testing.T) {
	remote, dir := cloneWithOrigin(t)
	localCommit(t, dir, "f.txt", "more\n", "more work")
	gitRun(t, dir, "tag", "back", "HEAD~1") // exists, but not at the tip
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"push","tags":["back","ghost"]}`), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, remote, "tag", "-l"); out != "" {
		t.Errorf("remote tags = %q, want none pushed", out)
	}
}
