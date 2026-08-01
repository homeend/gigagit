package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The conflict-complete lane runs a configured external agent headlessly
// against a paused sequencer op, so these tests configure trivial shell
// commands instead of a real agent — the same posture review_test.go takes.

// conflictAIRepo builds a paused, conflicted merge (main vs feature, both
// editing f.txt — conflict_test.go's conflictingRepo + conflictedMergeState
// fixture pair) whose .gg.toml declares tools, with both XDG roots isolated
// (reviewRepo's rationale: tool lists CONCATENATE across global+repo, and
// this is also where approvals persist).
func conflictAIRepo(t *testing.T, tools string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := conflictingRepo(t)
	if tools != "" {
		if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(tools), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-m", "config")
	}
	conflictedMergeState(t, dir)
	return dir
}

type conflictToolsResp struct {
	Op         string `json:"op"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Desc       string `json:"desc"`
	Conflicted int    `json:"conflicted"`
	Tools      []struct {
		Name     string `json:"name"`
		Command  string `json:"command"`
		Approved bool   `json:"approved"`
	} `json:"tools"`
}

func conflictTools(t *testing.T, ts *httptest.Server) (int, conflictToolsResp) {
	t.Helper()
	var body conflictToolsResp
	code := getJSON(t, ts, "/api/conflict/tools", &body)
	return code, body
}

// completeConflict posts a conflict-complete start and returns (status,
// decoded body).
func completeConflict(t *testing.T, ts *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/conflict/complete", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/conflict/complete: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// 1. Nothing paused: both endpoints refuse with 409.
func TestConflictToolsAndCompleteRequirePause(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := getJSON(t, ts, "/api/conflict/tools", nil); code != http.StatusConflict {
		t.Errorf("GET tools with nothing paused = %d, want 409", code)
	}
	if code, _ := completeConflict(t, ts, `{"tool":"whatever"}`); code != http.StatusConflict {
		t.Errorf("POST complete with nothing paused = %d, want 409", code)
	}
}

const terminalConflictTool = `
[[tools.command]]
category = "conflict_complete"
name = "Terminal"
mode = "terminal"
command = '''
echo hi
'''
`

const untaggedCaptureTool = `
[[tools.command]]
category = "conflict_complete"
name = "Untagged"
mode = "capture"
command = '''
echo gave up
'''
`

const tuiOnlyCaptureTool = `
[[tools.command]]
category = "conflict_complete"
name = "TuiOnly"
mode = "capture"
frontends = ["tui"]
command = '''
echo gave up
'''
`

const rebaseOnlyCaptureTool = `
[[tools.command]]
category = "conflict_complete"
name = "RebaseOnly"
mode = "capture"
frontends = ["web"]
when_op = "rebase"
command = '''
echo gave up
'''
`

// 2. Filtering: terminal hidden, untagged capture shown, tui-tagged hidden,
// when_op mismatch (rebase-only against a paused merge) hidden.
func TestConflictToolsFiltering(t *testing.T) {
	dir := conflictAIRepo(t, terminalConflictTool+untaggedCaptureTool+tuiOnlyCaptureTool+rebaseOnlyCaptureTool)
	ts := serve(t, New(domain.Open(dir)))

	code, got := conflictTools(t, ts)
	if code != http.StatusOK {
		t.Fatalf("GET tools = %d", code)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "Untagged" {
		t.Fatalf("tools = %+v, want only Untagged", got.Tools)
	}
	if got.Tools[0].Approved {
		t.Error("a never-run command reports itself approved")
	}
	if got.Tools[0].Command == "" {
		t.Error("resolved command is empty")
	}
	if got.Op != "merge" {
		t.Errorf("op = %q, want merge", got.Op)
	}
	if got.Conflicted < 1 {
		t.Errorf("conflicted = %d, want >= 1", got.Conflicted)
	}
}

// 3. Approval flow: unapproved → 403 needs_approval + resolved command;
// approve:true → 202; a second run of the same tool needs no approve flag
// (promptstate remembered). Uses a stop-early command so the op stays
// paused, letting a second run start without re-resolving the conflict.
func TestConflictCompleteApprovalGateThenRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := conflictAIRepo(t, untaggedCaptureTool)
	ts := serve(t, New(domain.Open(dir)))

	code, body := completeConflict(t, ts, `{"tool":"Untagged"}`)
	if code != http.StatusForbidden {
		t.Fatalf("unapproved start = %d, want 403", code)
	}
	if body["needs_approval"] != true {
		t.Fatalf("body = %v, want needs_approval", body)
	}
	if cmd, _ := body["command"].(string); !strings.Contains(cmd, "gave up") {
		t.Errorf("403 body does not carry the command to approve: %v", body)
	}

	code, body = completeConflict(t, ts, `{"tool":"Untagged","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("approved start = %d (%v), want 202", code, body)
	}
	opID, _ := body["op_id"].(string)
	done := readSSE(t, ts, opID, 30*time.Second)
	last := done[len(done)-1]
	if last["ok"] != true {
		t.Fatalf("done = %v", last)
	}
	if last["still_paused"] != true {
		t.Fatalf("still_paused = %v, want true (the stub never resolved anything)", last["still_paused"])
	}

	if code, _ := conflictTools(t, ts); code != http.StatusOK {
		t.Fatalf("tools after the stub run = %d, want 200 (still paused)", code)
	}
	// The remembered approval is what makes this second, flag-less start work.
	if code, body := completeConflict(t, ts, `{"tool":"Untagged"}`); code != http.StatusAccepted {
		t.Fatalf("second start = %d (%v), want 202 from the remembered approval", code, body)
	} else {
		readSSE(t, ts, body["op_id"].(string), 30*time.Second) // drain, so the lane is free
	}
}

const fakeAgentResolveTool = `
[[tools.command]]
category = "conflict_complete"
name = "Agent"
mode = "capture"
command = '''
git checkout --theirs f.txt && git add f.txt && GIT_EDITOR=true git merge --continue && printf 'took theirs\n' > "$GG_MESSAGE_FILE"
'''
`

// 4. A run that really resolves and continues the merge: done extra carries
// report/tool/op/still_paused, and /api/status no longer has a conflict
// object afterward.
func TestConflictCompleteRunResolvesMerge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := conflictAIRepo(t, fakeAgentResolveTool)
	ts := serve(t, New(domain.Open(dir)))

	code, body := completeConflict(t, ts, `{"tool":"Agent","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("start = %d (%v)", code, body)
	}
	opID, _ := body["op_id"].(string)
	done := readSSE(t, ts, opID, 30*time.Second)
	last := done[len(done)-1]
	if last["ok"] != true {
		t.Fatalf("done = %v", last)
	}
	if last["report"] != "took theirs" {
		t.Errorf("report = %v, want %q", last["report"], "took theirs")
	}
	if last["tool"] != "Agent" {
		t.Errorf("tool = %v, want Agent", last["tool"])
	}
	if last["op"] != "merge" {
		t.Errorf("op = %v, want merge", last["op"])
	}
	if last["still_paused"] != false {
		t.Errorf("still_paused = %v, want false", last["still_paused"])
	}
	// changed:true is what triggers the server-side commit-feed reset
	// (resetFeed in runOpStream) — this run created a merge commit, so a
	// later /api/commits must not serve a stale, pre-merge feed.
	if last["changed"] != true {
		t.Errorf("changed = %v, want true", last["changed"])
	}

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Conflict != nil {
		t.Errorf("conflict = %+v, want absent — the merge completed", st.Conflict)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("still unmerged:\n%s", out)
	}
	if log := gitRun(t, dir, "log", "--merges", "--oneline"); log == "" {
		t.Error("no merge commit after the agent completed the merge")
	}
}

// 5. Stop-early: an agent that reports without resolving anything leaves the
// op paused; /api/status still carries the conflict object.
func TestConflictCompleteStopEarlyLeavesPaused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := conflictAIRepo(t, untaggedCaptureTool)
	ts := serve(t, New(domain.Open(dir)))

	code, body := completeConflict(t, ts, `{"tool":"Untagged","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("start = %d (%v)", code, body)
	}
	done := readSSE(t, ts, body["op_id"].(string), 30*time.Second)
	last := done[len(done)-1]
	if last["ok"] != true {
		t.Fatalf("done = %v", last)
	}
	if last["still_paused"] != true {
		t.Errorf("still_paused = %v, want true", last["still_paused"])
	}

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Conflict == nil || st.Conflict.Op != "merge" {
		t.Fatalf("conflict = %+v, want the paused merge still reported", st.Conflict)
	}
}

const slowConflictTool = `
[[tools.command]]
category = "conflict_complete"
name = "Slow"
mode = "capture"
command = '''
sleep 30
'''
`

// 6. Cancel widens to conflict_complete runs; an ordinary git op still
// refuses (the review lane's existing rule, unaffected by the widening).
func TestConflictCompleteCancelAndGitOpStillRefused(t *testing.T) {
	dir := conflictAIRepo(t, slowConflictTool)
	ts := serve(t, New(domain.Open(dir)))

	code, body := completeConflict(t, ts, `{"tool":"Slow","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("start = %d (%v)", code, body)
	}
	opID, _ := body["op_id"].(string)
	var out struct{}
	if c := postJSON(t, ts, "/api/op/"+opID+"/cancel", `{}`, "application/json", "", &out); c != http.StatusOK {
		t.Fatalf("cancel = %d", c)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	last := events[len(events)-1]
	if last["ok"] != false || last["cancelled"] != true {
		t.Fatalf("done = %v, want a cancelled failure", last)
	}

	// A plain git op still refuses cancel — the lane is busy with an ordinary
	// op now (the previous run finished), reusing the merge-continue op.
	if out2 := gitRun(t, dir, "ls-files", "-u"); out2 == "" {
		t.Fatal("expected the merge to still be unresolved after a cancelled agent run")
	}
	opID2 := startOpBody(t, ts, `{"op":"continue"}`)
	if c := postJSON(t, ts, "/api/op/"+opID2+"/cancel", `{}`, "application/json", "", &out); c != http.StatusConflict {
		t.Fatalf("cancel of an ordinary op = %d, want 409", c)
	}
	readSSE(t, ts, opID2, 30*time.Second)
}

// 7. Unknown tool name → 400 (with something paused, so the check under test
// is name lookup, not the pause gate).
func TestConflictCompleteUnknownTool(t *testing.T) {
	dir := conflictAIRepo(t, untaggedCaptureTool)
	ts := serve(t, New(domain.Open(dir)))

	if code, _ := completeConflict(t, ts, `{"tool":"Nope"}`); code != http.StatusBadRequest {
		t.Errorf("unknown tool = %d, want 400", code)
	}
}
