package web

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
)

// TestAttachBrowserHost is not a test of anything by itself: with
// GG_BROWSER_CHECK=1 it hosts a real page, a real `sh` session and a real
// stream for the playwright check (web attach plan 1 has no web start
// path), prints the URL, and holds until the file GG_BROWSER_DONE names
// appears. Skipped otherwise.
func TestAttachBrowserHost(t *testing.T) {
	if os.Getenv("GG_BROWSER_CHECK") != "1" {
		t.Skip("GG_BROWSER_CHECK is not set")
	}
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	isolateGlobal(t)
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(t.Context())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	root := srv.service().Root()
	// A prompting shell: "$ " before every line, echo by the PTY, exit on
	// request — enough to see typing land and the session end.
	_, err := domain.Sessions().Start(domain.SessionStartSpec{
		Label: "sh", AgentID: "sh", Repo: "r", Dir: root, Cols: 80, Rows: 24,
		Argv: []string{"sh", "-c", `while true; do printf '$ '; read l || exit 0; eval "$l"; done`},
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("ATTACH_URL=%s\n", ts.URL)
	done := os.Getenv("GG_BROWSER_DONE")
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(done); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the browser check never finished")
}
