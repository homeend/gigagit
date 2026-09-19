package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type bookmarkCheckResp struct {
	Available bool   `json:"available"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
}

// A bookmark is a pointer. Clicking one whose commit was rebased away and gc'd
// used to do NOTHING in the browser (an unhandled 404), while the TUI printed
// git's raw error. /api/bookmarks/check is the one answer both now give.
func TestBookmarkCheckReportsAGoneCommit(t *testing.T) {
	dir, _, oldSha, tip := shelvedCommitRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	goneID := bookmarkCommit(t, ts, oldSha, "gone")
	liveID := bookmarkCommit(t, ts, tip, "live")

	gitRun(t, dir, "branch", "-D", "spike")
	gitRun(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitRun(t, dir, "gc", "--prune=now", "--quiet")
	if commitExists(t, dir, oldSha) {
		t.Fatalf("fixture: commit %s survived gc", oldSha)
	}

	var live bookmarkCheckResp
	if code := getJSON(t, ts, "/api/bookmarks/check?id="+liveID, &live); code != http.StatusOK || !live.Available {
		t.Fatalf("live bookmark: code = %d, got %+v", code, live)
	}
	var gone bookmarkCheckResp
	if code := getJSON(t, ts, "/api/bookmarks/check?id="+goneID, &gone); code != http.StatusOK {
		t.Fatalf("gone bookmark: code = %d", code)
	}
	if gone.Available || !strings.Contains(gone.Message, "no longer available") || gone.Detail == "" {
		t.Fatalf("gone bookmark = %+v, want unavailable with a message and a detail", gone)
	}
	if code := getJSON(t, ts, "/api/bookmarks/check?id=nope", nil); code != http.StatusNotFound {
		t.Fatalf("unknown id: code = %d, want 404", code)
	}
}
