package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// These tests swap the process-global task manager, so none runs in parallel.

func useWebTestTasks(t *testing.T) *domain.TaskManager {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	mgr := domain.NewTaskManager(nil)
	restore := domain.UseTaskManager(mgr)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mgr.KillAll(ctx)
		restore()
	})
	return mgr
}

func capture(kind exttool.Category, line string) config.ToolCommand {
	return config.ToolCommand{Category: string(kind), Name: "Test agent", Mode: "capture", Command: line}
}

func waitTask(t *testing.T, mgr *domain.TaskManager, id domain.TaskID, ok func(domain.TaskInfo) bool) domain.TaskInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		info, found := mgr.Get(id)
		if found && ok(info) {
			return info
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s: timed out, last %+v", id, info)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func ended(i domain.TaskInfo) bool   { return !i.State.Live() }
func running(i domain.TaskInfo) bool { return i.State == domain.TaskRunning }

type taskRowResp struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	State     string `json:"state"`
	Worktree  string `json:"worktree"`
	Live      bool   `json:"live"`
	HasResult bool   `json:"has_result"`
}

func listTasks(t *testing.T, ts *httptest.Server) []taskRowResp {
	t.Helper()
	var body struct {
		Tasks []taskRowResp `json:"tasks"`
	}
	if code := getJSON(t, ts, "/api/tasks", &body); code != http.StatusOK {
		t.Fatalf("GET /api/tasks = %d", code)
	}
	return body.Tasks
}

// The tab lists live tasks first, then ended ones (this process's, then the
// history), each saying whether enter has a result to show.
func TestTasksListLiveThenEnded(t *testing.T) {
	mgr := useWebTestTasks(t)
	dir := editRepo(t, 1)
	svc := domain.Open(dir)
	ts := serve(t, New(svc))
	spec, err := svc.CommitMessageTask(context.Background(), capture(exttool.CatCommitMessage, `echo "feat: from web"`))
	if err != nil {
		t.Fatal(err)
	}
	done := mgr.Submit(spec)
	waitTask(t, mgr, done, ended)
	rspec, err := svc.ReviewTask(context.Background(), capture(exttool.CatReview, "sleep 5"), domain.WorkingReviewTarget())
	if err != nil {
		t.Fatal(err)
	}
	live := mgr.Submit(rspec)
	waitTask(t, mgr, live, running)

	rows := listTasks(t, ts)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2", rows)
	}
	if rows[0].ID != string(live) || !rows[0].Live || rows[0].State != "running" {
		t.Errorf("first row = %+v, want the live review", rows[0])
	}
	if rows[1].ID != string(done) || rows[1].Live || rows[1].State != "done" || !rows[1].HasResult {
		t.Errorf("second row = %+v, want the done commit-message task with a result", rows[1])
	}
	if rows[1].Worktree == "" {
		t.Errorf("an ended row lost its worktree: %+v", rows[1])
	}
}

// enter's result: a commit message (apply offered in the served worktree), a
// failure's output tail, 404 for an unknown id or nothing to show.
func TestTaskResult(t *testing.T) {
	mgr := useWebTestTasks(t)
	dir := editRepo(t, 1)
	svc := domain.Open(dir)
	ts := serve(t, New(svc))
	ctx := context.Background()
	spec, _ := svc.CommitMessageTask(ctx, capture(exttool.CatCommitMessage, `echo "feat: from web"`))
	msg := mgr.Submit(spec)
	waitTask(t, mgr, msg, ended)
	fspec, _ := svc.CommitMessageTask(ctx, capture(exttool.CatCommitMessage, `echo boom-output; exit 3`))
	failed := mgr.Submit(fspec)
	if info := waitTask(t, mgr, failed, ended); info.State != domain.TaskFailed {
		t.Fatalf("the failing task ended %s", info.State)
	}

	var got struct {
		Title, Text, Ext string
		Apply            bool
	}
	if code := getJSON(t, ts, "/api/tasks/result?id="+string(msg), &got); code != http.StatusOK {
		t.Fatalf("result status = %d", code)
	}
	if !strings.Contains(got.Text, "feat: from web") || got.Ext != ".txt" || !got.Apply {
		t.Errorf("commit-message result = %+v, want its text, .txt, apply", got)
	}
	got = struct {
		Title, Text, Ext string
		Apply            bool
	}{}
	if code := getJSON(t, ts, "/api/tasks/result?id="+string(failed), &got); code != http.StatusOK {
		t.Fatalf("failure status = %d", code)
	}
	if !strings.Contains(got.Text, "boom-output") || got.Ext != ".log" || got.Apply {
		t.Errorf("failure result = %+v, want the output tail as .log, no apply", got)
	}
	if code := getJSON(t, ts, "/api/tasks/result?id=20260101-000000-nope", nil); code != http.StatusNotFound {
		t.Errorf("unknown id status = %d, want 404", code)
	}
}

// k k / x / s: cancel a live task, refuse to remove one, remove an ended one.
func TestTaskCancelAndRemove(t *testing.T) {
	mgr := useWebTestTasks(t)
	dir := editRepo(t, 1)
	svc := domain.Open(dir)
	ts := serve(t, New(svc))
	spec, _ := svc.ReviewTask(context.Background(), capture(exttool.CatReview, "sleep 5"), domain.WorkingReviewTarget())
	id := mgr.Submit(spec)
	waitTask(t, mgr, id, running)
	body := `{"id":"` + string(id) + `"}`
	if code, _ := postJSONRaw(t, ts, "/api/tasks/remove", body); code != http.StatusConflict {
		t.Fatalf("remove live status = %d, want 409", code)
	}
	if code, _ := postJSONRaw(t, ts, "/api/tasks/cancel", body); code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200", code)
	}
	if info := waitTask(t, mgr, id, ended); info.State != domain.TaskCancelled {
		t.Fatalf("state after cancel = %s", info.State)
	}
	if code, _ := postJSONRaw(t, ts, "/api/tasks/remove", body); code != http.StatusOK {
		t.Fatalf("remove ended status = %d, want 200", code)
	}
	for _, r := range mgr.History() {
		if r.ID == string(id) {
			t.Fatal("the removed record is still in the history")
		}
	}
	if code, _ := postJSONRaw(t, ts, "/api/tasks/cancel", `{"id":"20260101-000000-nope"}`); code != http.StatusNotFound {
		t.Errorf("cancel unknown status = %d, want 404", code)
	}
}

func TestTaskActionsWriteGuard(t *testing.T) {
	useWebTestTasks(t)
	ts := serve(t, New(domain.Open(editRepo(t, 1))))
	for _, path := range []string{"/api/tasks/cancel", "/api/tasks/remove", "/api/tasks/retry-save"} {
		if code := postJSON(t, ts, path, `{"id":"x"}`, "application/json", "http://evil.example", nil); code != http.StatusForbidden {
			t.Errorf("%s cross-origin status = %d, want 403", path, code)
		}
	}
}

// A task change reaches every tab as a "tasks" live event carrying the rows.
func TestTasksLiveEvent(t *testing.T) {
	isolateGlobal(t)
	mgr := useWebTestTasks(t)
	dir := editRepo(t, 1)
	svc := domain.Open(dir)
	srv := New(svc)
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	got := make(chan []liveMsg, 1)
	go func() { got <- readLiveSSE(t, ts, 2, 8*time.Second) }()
	time.Sleep(300 * time.Millisecond) // the stream's hello first
	spec, _ := svc.CommitMessageTask(context.Background(), capture(exttool.CatCommitMessage, `echo hi`))
	mgr.Submit(spec)
	msgs := <-got
	if msgs[1].Reason != "tasks" || len(msgs[1].Tasks) == 0 {
		t.Fatalf("second live message = %+v, want reason tasks with rows", msgs[1])
	}
}

// The client side of the tab and the ◆ rows is wired end to end.
func TestAITasksClientWired(t *testing.T) {
	for _, c := range []struct{ file, want, why string }{
		{"openfiles.js", "openTaskResult(t)", "enter shows a result"},
		{"openfiles.js", "taskAct(e.key)", "k / x / s act on the tasks tab"},
		{"openfiles.js", "footTasks()", "the foot shows only the keys that apply"},
		{"openfiles.js", `"gg:task"`, "a ◆ row opens the tab on its task"},
		{"tasks.js", "/api/tasks/result?id=", "the result comes from the server"},
		{"tasks.js", `"/api/tasks/" + path`, "cancel / remove / retry-save post to the server"},
		{"tasks.js", `$("commit-msg")`, "apply fills the commit box"},
		{"live.js", `msg.reason === "tasks"`, "a tasks event redraws the tab and the rows"},
		{"sidebar.js", "taskSubRows(w.path, cols)", "Worktrees rows carry their ◆ tasks"},
		{"sidebar.js", `"Cancel task"`, "a ◆ row's menu cancels"},
		{"style.css", "#taskresult.hidden { display: none; }", "the result overlay hides (no global .hidden rule)"},
		{"style.css", "#taskresult-apply.hidden { display: none; }", "apply hides off a commit message"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
