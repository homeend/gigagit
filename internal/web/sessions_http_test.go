package web

import (
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
)

// testSession starts `sh -c script` under a private manager and returns it.
func testSession(t *testing.T, script string) *domain.AgentSession {
	t.Helper()
	testSessionManager(t)
	s, err := domain.Sessions().Start(domain.SessionStartSpec{Label: "sh", AgentID: "sh", Repo: "r", Dir: t.TempDir(), Argv: []string{"sh", "-c", script}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// testSessionManager installs a private manager for the test.
func testSessionManager(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })
}

func TestSessionsListsEverySession(t *testing.T) {
	s := testSession(t, "sleep 5")
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var body struct{ Sessions []sessionWire }
	if code := getJSON(t, ts, "/api/sessions", &body); code != http.StatusOK || len(body.Sessions) != 1 {
		t.Fatalf("code=%d body=%+v", code, body)
	}
	got := body.Sessions[0]
	if got.ID != string(s.Info().ID) || got.Label != "sh" || got.State != "running" || got.Worktree != s.Info().Dir || got.Repo != "r" {
		t.Fatalf("%+v", got)
	}
}

func TestSessionsWireMarksTaskOwnedSessions(t *testing.T) {
	t.Parallel()
	list := []domain.SessionInfo{{ID: "s1", Label: "Claude", State: domain.SessionRunning}, {ID: "s2", Label: "codex", State: domain.SessionExited, ExitCode: 3}}
	tasks := []domain.TaskInfo{{ID: "t9", Key: "commit message — main @ abc1234", Session: "s1", State: domain.TaskRunning}}
	w := sessionsWire(list, tasks)
	if w[0].Task != "t9" || w[0].Label != "Claude · commit message — main @ abc1234" || w[1].Task != "" || w[1].State != "exited" || w[1].ExitCode != 3 {
		t.Fatalf("%+v", w)
	}
}

func TestTasksListIsReadOnly(t *testing.T) {
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var body struct{ Tasks []taskWire }
	if code := getJSON(t, ts, "/api/tasks", &body); code != http.StatusOK || body.Tasks == nil {
		t.Fatalf("code=%d body=%+v", code, body)
	}
	if code, _ := postJSONRaw(t, ts, "/api/tasks", `{}`); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/tasks = %d, want 405", code)
	}
}

func TestSessionStartAndExitEmitSessionsEvents(t *testing.T) {
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
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = domain.Sessions().Start(domain.SessionStartSpec{Label: "sh", Dir: t.TempDir(), Argv: []string{"sh", "-c", "exit 0"}, Cols: 20, Rows: 5})
	}()
	msgs := readLiveSSE(t, ts, 3, 5*time.Second) // hello, sessions (start), sessions (exit)
	if len(msgs) < 3 || msgs[1].Reason != "sessions" || msgs[2].Reason != "sessions" || len(msgs[2].Sessions) != 1 || msgs[2].Sessions[0].State != "exited" {
		t.Fatalf("%+v", msgs)
	}
}
