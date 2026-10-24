package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// The launch dialog's tests swap the process-global task manager: none runs
// in parallel.

const launchTools = `
[[tools.command]]
category = "commit_message"
name = "Slow"
mode = "capture"
command = "echo 'feat: from the dialog'"

[[tools.command]]
category = "commit_message"
name = "Inter"
mode = "interactive"
command = "sh -c 'exit 0'"

[[tools.command]]
category = "commit_message"
name = "Asks"
mode = "capture"
command = "echo <user:Topic>"

[[tools.command]]
category = "commit_message"
name = "Gone"
mode = "capture"
command = "gg-no-such-program-xyz"

[[tools.command]]
category = "review"
name = "Rev"
mode = "capture"
command = "echo looks fine"
`

// launchServer: a repo with launchTools in its .gg.toml, one staged file, no
// agent detection, approvals in a temp state dir.
func launchServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	dir := editRepo(t, 1)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(launchTools), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "staged.txt", "x\n")
	gitRun(t, dir, "add", "staged.txt")
	srv := New(domain.Open(dir))
	srv.detectTools = func() []exttool.Detection { return nil }
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	return srv, serve(t, srv), dir
}

type launchResp struct {
	Key     string `json:"key"`
	Choices []struct {
		Agent    string `json:"agent"`
		Identity string `json:"identity"`
		Rows     []struct {
			Mode    string `json:"mode"`
			Name    string `json:"name"`
			None    bool   `json:"none"`
			Missing bool   `json:"missing"`
		} `json:"rows"`
	} `json:"choices"`
	Pick struct{ Identity, Mode, Name string } `json:"pick"`
	Wait string                                `json:"wait"`
}

func getLaunch(t *testing.T, ts *httptest.Server, query string) launchResp {
	t.Helper()
	var got launchResp
	if code := getJSON(t, ts, "/api/tasks/launch?"+query, &got); code != http.StatusOK {
		t.Fatalf("GET launch %s = %d", query, code)
	}
	return got
}

// The dialog offers the TUI's rows: one agent per custom command, every mode
// (a mode without a command greyed), a missing program marked, a command that
// asks for <user:…> input left out.
func TestTaskLaunchChoices(t *testing.T) {
	useWebTestTasks(t)
	_, ts, _ := launchServer(t)
	got := getLaunch(t, ts, "kind=commit_message")
	if !strings.HasPrefix(got.Key, "commit message — ") {
		t.Errorf("key = %q, want the task key", got.Key)
	}
	var agents []string
	rows := map[string]string{}
	for _, c := range got.Choices {
		agents = append(agents, c.Agent)
		var r []string
		for _, row := range c.Rows {
			s := row.Mode
			if row.None {
				s += ":none"
			} else {
				s += ":" + row.Name
			}
			if row.Missing {
				s += ":missing"
			}
			r = append(r, s)
		}
		rows[c.Agent] = strings.Join(r, ",")
	}
	if strings.Join(agents, ",") != "Slow,Inter,Gone" {
		t.Fatalf("agents = %v, want Slow,Inter,Gone (Asks needs input)", agents)
	}
	if want := "background:none,foreground:none,headless:Slow"; rows["Slow"] != want {
		t.Errorf("Slow rows = %q, want %q", rows["Slow"], want)
	}
	if want := "background:Inter,foreground:Inter,headless:none"; rows["Inter"] != want {
		t.Errorf("Inter rows = %q, want %q", rows["Inter"], want)
	}
	if !strings.Contains(rows["Gone"], "headless:Gone:missing") {
		t.Errorf("Gone rows = %q, want the missing program marked", rows["Gone"])
	}
}

func submitBody(identity, mode, name string, approve bool) string {
	a := "false"
	if approve {
		a = "true"
	}
	return `{"kind":"commit_message","identity":"` + identity + `","mode":"` + mode + `","name":"` + name + `","approve":` + a + `}`
}

// enter runs: an unapproved command is shown first (403 needs_approval with
// the resolved text); approved, the task goes to the scheduler, its result is
// the commit message, and the choice is remembered for the next launch.
func TestTaskSubmitHeadless(t *testing.T) {
	mgr := useWebTestTasks(t)
	_, ts, _ := launchServer(t)
	code, body := postJSONAny(t, ts, "/api/tasks/submit", submitBody("name:Slow", "headless", "Slow", false))
	if code != http.StatusForbidden || body["needs_approval"] != true || !strings.Contains(body["command"].(string), "feat: from the dialog") {
		t.Fatalf("unapproved submit = %d %v, want 403 needs_approval with the command", code, body)
	}
	if len(mgr.List()) != 0 {
		t.Fatal("an unapproved submit queued a task")
	}
	code, body = postJSONAny(t, ts, "/api/tasks/submit", submitBody("name:Slow", "headless", "Slow", true))
	if code != http.StatusOK {
		t.Fatalf("approved submit = %d %v", code, body)
	}
	id := domain.TaskID(body["id"].(string))
	info := waitTask(t, mgr, id, ended)
	if info.State != domain.TaskDone || !strings.Contains(info.Result, "feat: from the dialog") {
		t.Fatalf("task ended %s with %q", info.State, info.Result)
	}
	// approved once: the second submit needs no approve flag
	if code, body = postJSONAny(t, ts, "/api/tasks/submit", submitBody("name:Slow", "headless", "Slow", false)); code != http.StatusOK {
		t.Fatalf("second submit = %d %v, want the approval remembered", code, body)
	}
	got := getLaunch(t, ts, "kind=commit_message")
	if got.Pick.Identity != "name:Slow" || got.Pick.Mode != "headless" || got.Pick.Name != "Slow" {
		t.Errorf("remembered pick = %+v", got.Pick)
	}
}

func TestTaskSubmitRefusals(t *testing.T) {
	mgr := useWebTestTasks(t)
	_, ts, _ := launchServer(t)
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"a missing program", submitBody("name:Gone", "headless", "Gone", true), http.StatusUnprocessableEntity},
		{"a mode the agent has no command for", submitBody("name:Slow", "background", "Slow", true), http.StatusBadRequest},
		{"an unknown agent", submitBody("name:Nope", "headless", "Nope", true), http.StatusBadRequest},
		{"a command that needs input", submitBody("name:Asks", "headless", "Asks", true), http.StatusBadRequest},
		{"an unknown kind", `{"kind":"session","identity":"name:Slow","mode":"headless","name":"Slow","approve":true}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, body := postJSONAny(t, ts, "/api/tasks/submit", tc.body); code != tc.want {
				t.Fatalf("status = %d %v, want %d", code, body, tc.want)
			}
		})
	}
	if len(mgr.List()) != 0 {
		t.Fatalf("a refused submit queued a task: %+v", mgr.List())
	}
	if code := postJSON(t, ts, "/api/tasks/submit", submitBody("name:Slow", "headless", "Slow", true), "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Fatalf("cross-origin submit = %d, want 403", code)
	}
}

// A review task stores its result as a review note.
func TestTaskSubmitReview(t *testing.T) {
	mgr := useWebTestTasks(t)
	srv, ts, _ := launchServer(t)
	srv.service().UseNotesDir(t.TempDir())
	code, body := postJSONAny(t, ts, "/api/tasks/submit", `{"kind":"review","target":"working","identity":"name:Rev","mode":"headless","name":"Rev","approve":true}`)
	if code != http.StatusOK {
		t.Fatalf("review submit = %d %v", code, body)
	}
	info := waitTask(t, mgr, domain.TaskID(body["id"].(string)), ended)
	if info.State != domain.TaskDone || info.NoteID == "" {
		t.Fatalf("review ended %s, note %q, err %q save %q", info.State, info.NoteID, info.Err, info.SaveErr)
	}
	if !strings.HasPrefix(info.Key, "review — ") {
		t.Errorf("key = %q", info.Key)
	}
}

// The wait line: a task with the same key ahead.
func TestTaskLaunchWaitLine(t *testing.T) {
	mgr := useWebTestTasks(t)
	srv, ts, _ := launchServer(t)
	spec, err := srv.service().CommitMessageTask(t.Context(), capture(exttool.CatCommitMessage, "sleep 5"))
	if err != nil {
		t.Fatal(err)
	}
	waitTask(t, mgr, mgr.Submit(spec), running)
	if got := getLaunch(t, ts, "kind=commit_message"); !strings.Contains(got.Wait, "with this key ahead") {
		t.Errorf("wait = %q, want the same-key line", got.Wait)
	}
}

// Every web AI action opens the launch dialog, and this tab applies its own
// tasks' results from the live rows.
func TestLaunchDialogWired(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"review.js", `return openLaunch({ kind: "review", target, branch, sha, preview, shas });`, "every review row opens the dialog"},
		{"review.js", `{ kind: "conflict", label: "Resolve with an agent…" }`, "the tools picker offers a resolve agent"},
		{"review.js", `{ kind: "conflict_complete", label: "Resolve & complete with an agent…" }`, "…and a resolve & complete agent"},
		{"review.js", `act: () => openLaunch({ kind: k.kind })`, "either row opens the launch dialog"},
		{"launch.js", `(m.kind === "conflict" || m.kind === "conflict_complete")`, "a resolve run that ends with no overview reloads the status"},
		{"commitai.js", `openLaunch({ kind: "commit_message" });`, "generate opens the dialog"},
		{"commitai.js", `"nothing staged to describe"`, "generate refuses with nothing staged (the TUI's words)"},
		{"live.js", "launchTasks(msg.tasks || [])", "a tasks event applies this tab's results"},
		{"launch.js", `"/api/tasks/launch?"`, "the dialog lays itself out from the server"},
		{"launch.js", `"/api/tasks/submit"`, "enter submits to the scheduler"},
		{"launch.js", "e.data.needs_approval", "an unapproved command is shown before it runs"},
		{"launch.js", "if (t.results > m.seen)", "each result is applied once"},
		{"launch.js", "openConsole(t.session)", "a foreground task's console opens"},
		// a review the store could not keep says so and points at the retry
		// (the TUI's applyReviewResult; s in the AI tasks tab retries)
		{"launch.js", `toast(t.key + ": " + t.save_err + " — ctrl+\\ to retry", { err: true })`, "a failed review save points at the retry"},
		{"launch.js", `Array.isArray(v) ? v.join(",") : v`, "the marked range's commits reach the server"},
		// review fixes: one submit per pick, the task never lost; results survive
		// a reload; an operation's decision is never taken over; a review never
		// replaces a diff being read or a field being typed in; the grid size
		{"launch.js", "if (!dlg || dlg.busy) return;", "a second enter while a submit is in flight starts nothing"},
		{"launch.js", "if (dlg || opening) return;", "a double click on a door opens one dialog"},
		{"launch.js", "sessionStorage.setItem(MINE_KEY", "this tab's tasks survive a reload (a repo switch reloads)"},
		{"launch.js", "launchTasks(state.tasks || []);", "a task that ended before the submit reply is caught up"},
		{"launch.js", `if (state.op || layers.some((l) => l.id === "modal")) return toast(`, "an offer never takes over an operation's decision"},
		{"launch.js", `state.layout !== "diff"`, "a review result waits while a diff is being read"},
		{"launch.js", "...measure()", "an interactive task starts at the console's grid size"},
		{"style.css", "#launch.hidden { display: none; }", "the dialog hides (no global .hidden rule)"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

const launchConflictTools = `
[[tools.command]]
category = "conflict_complete"
name = "Terminal"
mode = "terminal"
command = "echo hi"

[[tools.command]]
category = "conflict_complete"
name = "Untagged"
mode = "capture"
command = "echo gave up"

[[tools.command]]
category = "conflict_complete"
name = "TuiOnly"
mode = "capture"
frontends = ["tui"]
command = "echo gave up"

[[tools.command]]
category = "conflict_complete"
name = "RebaseOnly"
mode = "capture"
frontends = ["web"]
when_op = "rebase"
command = "echo gave up"

[[tools.command]]
category = "conflict_complete"
name = "Agent"
mode = "capture"
command = '''
git checkout --theirs f.txt && git add f.txt && GIT_EDITOR=true git merge --continue && printf 'took theirs\n' > "$GG_MESSAGE_FILE"
'''
`

// conflictLaunchServer: a paused, conflicted merge with launchConflictTools.
func conflictLaunchServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := conflictingRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(launchConflictTools), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "config")
	conflictedMergeState(t, dir)
	srv := New(domain.Open(dir))
	srv.detectTools = func() []exttool.Detection { return nil }
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	return serve(t, srv), dir
}

// The AI resolve dialog: a whole-operation terminal agent is an interactive
// one (the TUI's TaskModeOf), a tui-only command and a when_op that does not
// match the paused merge are left out.
func TestConflictLaunchChoices(t *testing.T) {
	useWebTestTasks(t)
	ts, _ := conflictLaunchServer(t)
	got := getLaunch(t, ts, "kind=conflict_complete")
	var agents []string
	for _, c := range got.Choices {
		agents = append(agents, c.Agent)
	}
	if strings.Join(agents, ",") != "Terminal,Untagged,Agent" {
		t.Fatalf("agents = %v, want Terminal,Untagged,Agent", agents)
	}
	if r := got.Choices[0].Rows; r[0].None || r[0].Name != "Terminal" || !r[2].None {
		t.Errorf("Terminal rows = %+v, want interactive only", r)
	}
}

// Nothing paused: nothing to resolve.
func TestConflictLaunchRequiresPause(t *testing.T) {
	useWebTestTasks(t)
	_, ts, _ := launchServer(t)
	var body map[string]any
	if code := getJSON(t, ts, "/api/tasks/launch?kind=conflict_complete", &body); code == http.StatusOK {
		t.Fatalf("launch with nothing paused = %d %v, want a refusal", code, body)
	}
}

// A headless resolve & complete agent finishes the merge; its overview is the
// task's result.
func TestConflictTaskResolvesMerge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	mgr := useWebTestTasks(t)
	ts, dir := conflictLaunchServer(t)
	code, body := postJSONAny(t, ts, "/api/tasks/submit", `{"kind":"conflict_complete","identity":"name:Agent","mode":"headless","name":"Agent","approve":true}`)
	if code != http.StatusOK {
		t.Fatalf("submit = %d %v", code, body)
	}
	info := waitTask(t, mgr, domain.TaskID(body["id"].(string)), ended)
	if info.State != domain.TaskDone || info.Result != "took theirs" {
		t.Fatalf("task ended %s with %q (err %q)", info.State, info.Result, info.Err)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("still unmerged:\n%s", out)
	}
	if log := gitRun(t, dir, "log", "--merges", "--oneline"); log == "" {
		t.Error("no merge commit after the agent completed the merge")
	}
}

// The frontends filter: a review tool limited to the TUI is not offered.
func TestTaskLaunchHidesTuiOnlyTools(t *testing.T) {
	useWebTestTasks(t)
	dir := reviewRepo(t, echoReviewTool+`
[[tools.command]]
category = "review"
name = "TuiOnly"
mode = "capture"
frontends = ["tui"]
command = "echo hidden"
`)
	srv := New(domain.Open(dir))
	srv.detectTools = func() []exttool.Detection { return nil }
	ts := serve(t, srv)
	got := getLaunch(t, ts, "kind=review&target=working")
	if len(got.Choices) != 1 || got.Choices[0].Agent != "Echo" {
		t.Fatalf("choices = %+v, want only Echo", got.Choices)
	}
}

// An approval covers the command TEXT: editing the command asks again.
func TestTaskApprovalKeyedOnCommandText(t *testing.T) {
	useWebTestTasks(t)
	srv, ts, dir := launchServer(t)
	_ = srv
	if code, body := postJSONAny(t, ts, "/api/tasks/submit", submitBody("name:Slow", "headless", "Slow", true)); code != http.StatusOK {
		t.Fatalf("approved submit = %d %v", code, body)
	}
	edited := strings.Replace(launchTools, "echo 'feat: from the dialog'", "echo 'feat: edited'", 1)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := postJSONAny(t, ts, "/api/tasks/submit", submitBody("name:Slow", "headless", "Slow", false)); code != http.StatusForbidden || body["needs_approval"] != true {
		t.Fatalf("edited command submit = %d %v, want 403 needs_approval", code, body)
	}
}

const launchResolveTools = `
[[tools.command]]
category = "conflict"
name = "Resolver"
mode = "capture"
command = '''
git checkout --theirs f.txt && git add f.txt && printf 'kept theirs, not continued\n' > "$GG_MESSAGE_FILE"
'''

[[tools.command]]
category = "conflict"
name = "Meld"
mode = "terminal"
per_file = true
command = "meld <local> <merged> <remote>"
`

// "Resolve with an agent…" (kind conflict): the agent resolves the files and
// never continues the operation; a per-file mergetool is not an agent (it
// stays in the tools picker's in-place rows).
func TestConflictResolveLaunchAndRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	mgr := useWebTestTasks(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := conflictingRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(launchResolveTools), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "config")
	conflictedMergeState(t, dir)
	srv := New(domain.Open(dir))
	srv.detectTools = func() []exttool.Detection { return nil }
	ts := serve(t, srv)

	got := getLaunch(t, ts, "kind=conflict")
	if !strings.HasPrefix(got.Key, "resolve conflict — ") {
		t.Errorf("key = %q", got.Key)
	}
	if len(got.Choices) != 1 || got.Choices[0].Agent != "Resolver" {
		t.Fatalf("choices = %+v, want only Resolver (Meld is per-file)", got.Choices)
	}
	code, body := postJSONAny(t, ts, "/api/tasks/submit", `{"kind":"conflict","identity":"name:Resolver","mode":"headless","name":"Resolver","approve":true}`)
	if code != http.StatusOK {
		t.Fatalf("submit = %d %v", code, body)
	}
	info := waitTask(t, mgr, domain.TaskID(body["id"].(string)), ended)
	if info.State != domain.TaskDone || !strings.Contains(info.Result, "kept theirs") {
		t.Fatalf("task ended %s with %q (err %q)", info.State, info.Result, info.Err)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("still unmerged:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); err != nil {
		t.Error("the merge was continued — a resolve agent must leave it paused")
	}
}
