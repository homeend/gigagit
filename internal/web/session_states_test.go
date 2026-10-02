package web

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
)

func TestSessionsWireCarriesActivity(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	list := []domain.SessionInfo{{ID: "s1", Label: "Claude", State: domain.SessionRunning}, {ID: "s2", Label: "codex", State: domain.SessionRunning}}
	act := map[domain.SessionID]domain.SessionActivity{
		"s1": {State: domain.ActivityQuestion, Since: since, StepFor: 7 * time.Second, Stalled: true,
			Options: []domain.ActivityOption{{Key: "1", Label: "Yes"}}},
	}
	w := sessionsWireWith(list, nil, func(id domain.SessionID) (domain.SessionActivity, bool) { a, ok := act[id]; return a, ok })
	if w[0].AgentState != "question" || !w[0].Since.Equal(since) || w[0].StepFor != 7 || !w[0].Stalled || len(w[0].Options) != 1 {
		t.Fatalf("classified: %+v", w[0])
	}
	raw, _ := json.Marshal(w[1])
	for _, k := range []string{"agent_state", "since", "step_for", "stalled", "options"} {
		if strings.Contains(string(raw), `"`+k+`"`) {
			t.Errorf("unclassified row carries %s: %s", k, raw)
		}
	}
}

// Serial: swaps the manager and the state watcher. A notice reaches the tabs
// once, on the sessions event that follows it; one posted before the server
// started is never replayed.
func TestStateNoticesRideTheSessionsEvent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	isolateGlobal(t)
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })
	w := domain.NewStaticStates(nil)
	restoreW := domain.UseSessionStates(w)
	t.Cleanup(restoreW)
	w.PostNotice(domain.ActivityNotice{ID: "s0", Kind: "idle", Label: "Old", Dir: "/wt/old"})
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(t.Context())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	go func() {
		time.Sleep(200 * time.Millisecond)
		w.PostNotice(domain.ActivityNotice{ID: "s1", Kind: "question", Label: "Claude", Dir: "/wt/a"})
		time.Sleep(200 * time.Millisecond)
		w.PostNotice(domain.ActivityNotice{ID: "s1", Kind: "stalled", Label: "Claude", Dir: "/wt/a", Quiet: 125 * time.Second})
	}()
	msgs := readLiveSSE(t, ts, 3, 5*time.Second) // hello, sessions (question), sessions (stalled)
	if len(msgs) < 3 || msgs[1].Reason != "sessions" || len(msgs[1].Notices) != 1 || msgs[1].Notices[0].Kind != "question" || msgs[1].Notices[0].Label != "Claude" {
		t.Fatalf("first: %+v", msgs)
	}
	if len(msgs[2].Notices) != 1 || msgs[2].Notices[0].Kind != "stalled" || msgs[2].Notices[0].QuietS != 125 {
		t.Fatalf("second: %+v", msgs[2].Notices)
	}
}

const badRulesTool = "[[tools.command]]\ncategory = \"session\"\nmode = \"session\"\nname = \"Shell\"\n" +
	"screen_working = [\"(\"]\ncommand = '''sh -c 'sleep 30' '''\n"

func TestSessionStartWarnsAboutBadScreenRules(t *testing.T) {
	srv, root := lifecycleServer(t, badRulesTool)
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","approve":true`))
	if code != http.StatusOK {
		t.Fatalf("start = %d %v", code, body)
	}
	if warn, _ := body["warning"].(string); !strings.Contains(warn, "Shell") || !strings.Contains(warn, "built-in") {
		t.Fatalf("warning = %q", warn)
	}
	if n := len(domain.Sessions().List()); n != 1 {
		t.Fatalf("%d sessions", n)
	}
}
