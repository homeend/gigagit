package web

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
)

// TestAttachBrowserHost is not a test of anything by itself: with
// GG_BROWSER_CHECK=1 it hosts a real page, a real `sh` session and a real
// stream for the playwright checks (plan 1's attach, plan 2's lifecycle —
// which starts its own session from the seeded "Shell" command), prints the
// URL, and holds until the file GG_BROWSER_DONE names
// appears. Skipped otherwise.
func TestAttachBrowserHost(t *testing.T) {
	if os.Getenv("GG_BROWSER_CHECK") != "1" {
		t.Skip("GG_BROWSER_CHECK is not set")
	}
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	global := isolateGlobal(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // the approval store
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	// A session command that needs no detection and no real agent (the
	// lifecycle check starts it from the worktree menu).
	const seeded = "[[tools.command]]\ncategory = \"session\"\nmode = \"session\"\nname = \"Shell\"\n" +
		"command = '''sh -c 'while true; do printf \"$ \"; read l || exit 0; eval \"$l\"; done' '''\n"
	if err := os.WriteFile(global, []byte(seeded), 0o644); err != nil {
		t.Fatal(err)
	}
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
