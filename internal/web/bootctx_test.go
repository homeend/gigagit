package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// serveDirect runs one request through the full mux with the given context —
// the httptest.Server round-trip can't hand us a pre-cancelled request, but a
// direct ServeHTTP can, and that models the production failure exactly: the
// browser aborted the page load (F5) while the read was in flight.
func serveDirect(t *testing.T, srv *Server, ctx context.Context, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil).WithContext(ctx)
	req.Host = "127.0.0.1:8899" // pass the hostGuard (DNS-rebinding defense)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// Boot-critical reads must survive the request being aborted: these reads
// coalesce across page loads (domain singleflight), and a follower inherits
// the leader's error — so a cancelled leader poisoned the NEXT load's
// identical request ("context canceled" in 0.5s while the real read needed a
// minute on a big repo; the every-other-F5 wireframes report). Detached from
// the request context, the read completes regardless and the reload joins it.
func TestBootReadsSurviveAbortedRequest(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for _, path := range []string{
		"/api/repo",
		"/api/status",
		"/api/branches",
		"/api/worktrees",
		"/api/tags",
		"/api/stashes",
		"/api/health",
		"/api/commits",
	} {
		if rec := serveDirect(t, srv, cancelled, path); rec.Code != http.StatusOK {
			t.Errorf("%s with an aborted request ctx = %d, want 200 (body: %s)", path, rec.Code, rec.Body.String())
		}
	}
}

// User-driven detail reads keep request cancellation on purpose (abandoning a
// diff must free its git read); this pins that the detachment above did not
// leak into them.
func TestDetailReadsStayCancellable(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	head := gitRun(t, dir, "rev-parse", "HEAD")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if rec := serveDirect(t, srv, cancelled, "/api/commit/"+head); rec.Code == http.StatusOK {
		t.Errorf("/api/commit/{sha} with an aborted request ctx = 200, want an error (cancellation must still apply)")
	}
}
