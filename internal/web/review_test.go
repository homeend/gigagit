package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The review lane runs a configured external command, so these tests
// configure a trivial shell command instead of a real agent: the plumbing
// under test is target resolution, tool selection, the approval gate and the
// report round trip — none of which care what the tool is.

// reviewRepo builds a two-branch repo whose .gg.toml declares one review
// tool. body is the command; it runs through the same capture runner a real
// agent would.
func reviewRepo(t *testing.T, tools string) string {
	t.Helper()
	// Isolate BOTH config roots: tool lists CONCATENATE across global+repo, so
	// without this the developer's own review tools would join the fixture's.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // reports and the approval store
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "feature work")
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(tools), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "config")
	return dir
}

const echoReviewTool = `
[[tools.command]]
category = "review"
name = "Echo"
mode = "capture"
command = '''
printf '# review of <range>\nlooks fine\n'
'''
`

type reviewToolsResp struct {
	Label string `json:"label"`
	Range string `json:"range"`
	Tools []struct {
		Name     string `json:"name"`
		Command  string `json:"command"`
		Approved bool   `json:"approved"`
	} `json:"tools"`
}

func reviewTools(t *testing.T, ts *httptest.Server, query string) reviewToolsResp {
	t.Helper()
	var body reviewToolsResp
	if code := getJSON(t, ts, "/api/review/tools"+query, &body); code != http.StatusOK {
		t.Fatalf("GET /api/review/tools%s = %d", query, code)
	}
	return body
}

// startReview posts a review start and returns (status, decoded body).
func startReview(t *testing.T, ts *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	req := strings.NewReader(body)
	resp, err := http.Post(ts.URL+"/api/review", "application/json", req)
	if err != nil {
		t.Fatalf("POST /api/review: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestReviewToolsListsConfigured(t *testing.T) {
	dir := reviewRepo(t, echoReviewTool)
	ts := serve(t, New(domain.Open(dir)))

	got := reviewTools(t, ts, "?target=branch&branch=feature")
	if len(got.Tools) != 1 || got.Tools[0].Name != "Echo" {
		t.Fatalf("tools = %+v, want the one configured Echo", got.Tools)
	}
	if got.Tools[0].Approved {
		t.Error("a never-run command reports itself approved")
	}
	// The branch's LABEL is its name; the RANGE is pure hex — the split that
	// keeps a ref name out of the command line.
	if got.Label != "feature" {
		t.Errorf("label = %q, want feature", got.Label)
	}
	if !strings.Contains(got.Range, "..") || strings.Contains(got.Range, "feature") {
		t.Errorf("range = %q, want a hex A..B", got.Range)
	}
	// The command shown for approval is the resolved one, not the template.
	if !strings.Contains(got.Tools[0].Command, got.Range) {
		t.Errorf("resolved command %q does not carry the range %q", got.Tools[0].Command, got.Range)
	}
}

// Working changes have no range at all: the tool reads the diff from
// $GG_REVIEW_DIFF instead, so <range> resolves empty.
func TestReviewToolsWorkingTarget(t *testing.T) {
	dir := reviewRepo(t, echoReviewTool)
	ts := serve(t, New(domain.Open(dir)))

	got := reviewTools(t, ts, "?target=working")
	if got.Label != "working changes" {
		t.Errorf("label = %q, want working changes", got.Label)
	}
	if got.Range != "" {
		t.Errorf("range = %q, want empty", got.Range)
	}
}

const tuiOnlyReviewTool = `
[[tools.command]]
category = "review"
name = "TuiOnly"
mode = "capture"
frontends = ["tui"]
command = '''
printf '# review of <range>\nlooks fine\n'
'''
`

// A review block tagged frontends=["tui"] must be invisible to the web
// tools listing — it stays hidden alongside the one web-visible tool.
func TestReviewToolsHidesTuiOnlyFrontend(t *testing.T) {
	dir := reviewRepo(t, echoReviewTool+tuiOnlyReviewTool)
	ts := serve(t, New(domain.Open(dir)))

	got := reviewTools(t, ts, "?target=branch&branch=feature")
	if len(got.Tools) != 1 || got.Tools[0].Name != "Echo" {
		t.Fatalf("tools = %+v, want only Echo (TuiOnly is frontends=[\"tui\"])", got.Tools)
	}
}

func TestReviewToolsEmptyWithoutConfig(t *testing.T) {
	dir := reviewRepo(t, "")
	ts := serve(t, New(domain.Open(dir)))

	if got := reviewTools(t, ts, "?target=branch&branch=feature"); len(got.Tools) != 0 {
		t.Errorf("tools = %+v, want none", got.Tools)
	}
	code, body := startReview(t, ts, `{"target":"branch","branch":"feature"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("start with no tool configured = %d, want 400", code)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "no review tool configured") {
		t.Errorf("error = %q", msg)
	}
}

// The approval gate: the first run is refused with the command to show, the
// approved run produces a report, and the approval is REMEMBERED — a second
// run needs no approve flag.
func TestReviewApprovalGateThenRun(t *testing.T) {
	dir := reviewRepo(t, echoReviewTool)
	ts := serve(t, New(domain.Open(dir)))

	code, body := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Echo"}`)
	if code != http.StatusForbidden {
		t.Fatalf("unapproved start = %d, want 403", code)
	}
	if body["needs_approval"] != true {
		t.Fatalf("body = %v, want needs_approval", body)
	}
	if cmd, _ := body["command"].(string); !strings.Contains(cmd, "printf") {
		t.Errorf("403 body does not carry the command to approve: %v", body)
	}

	code, body = startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Echo","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("approved start = %d (%v), want 202", code, body)
	}
	opID, _ := body["op_id"].(string)
	done := readSSE(t, ts, opID, 30*time.Second)
	last := done[len(done)-1]
	if last["ok"] != true {
		t.Fatalf("done = %v", last)
	}
	report, _ := last["report"].(string)
	if !strings.Contains(report, "looks fine") {
		t.Errorf("report = %q, want the tool's output", report)
	}
	// A review changes nothing, so the feed must not be invalidated.
	if last["changed"] != false {
		t.Errorf("changed = %v, want false", last["changed"])
	}
	path, _ := last["path"].(string)
	if path == "" {
		t.Fatal("done carries no report path")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("persisted report: %v", err)
	}
	if string(onDisk) != report {
		t.Errorf("persisted %q != streamed %q", onDisk, report)
	}

	if got := reviewTools(t, ts, "?target=branch&branch=feature"); !got.Tools[0].Approved {
		t.Error("the approved command still reports itself unapproved")
	}
	// The remembered approval is what makes this second, flag-less start work.
	if code, body := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Echo"}`); code != http.StatusAccepted {
		t.Fatalf("second start = %d (%v), want 202 from the remembered approval", code, body)
	}
}

// An edited command is a different command: its hash changes, so the
// approval no longer covers it.
func TestReviewApprovalKeyedOnCommandText(t *testing.T) {
	dir := reviewRepo(t, echoReviewTool)
	ts := serve(t, New(domain.Open(dir)))
	if code, _ := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Echo","approve":true}`); code != http.StatusAccepted {
		t.Fatal("approved start failed")
	}
	readSSE(t, ts, "op1", 30*time.Second) // drain, so the lane is free

	edited := strings.Replace(echoReviewTool, "looks fine", "looks different", 1)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Echo"}`); code != http.StatusForbidden {
		t.Errorf("edited command start = %d, want 403 — approval must not survive an edit", code)
	}
}

func TestReviewRejectsUnknownToolAndBranch(t *testing.T) {
	dir := reviewRepo(t, echoReviewTool)
	ts := serve(t, New(domain.Open(dir)))

	if code, _ := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Nope"}`); code != http.StatusBadRequest {
		t.Errorf("unknown tool = %d, want 400", code)
	}
	if code, _ := startReview(t, ts, `{"target":"branch","branch":"--upload-pack=x","tool":"Echo"}`); code != http.StatusBadRequest {
		t.Errorf("leading-dash branch = %d, want 400", code)
	}
	if code, _ := startReview(t, ts, `{"target":"branch","branch":"no-such-branch","tool":"Echo"}`); code != http.StatusBadRequest {
		t.Errorf("unknown branch = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/review/tools?target=branch&branch=-x", nil); code != http.StatusBadRequest {
		t.Errorf("tools with a dash branch = %d, want 400", code)
	}
}

// A tool that produces nothing is a failed review, not an empty report file.
func TestReviewEmptyOutputFails(t *testing.T) {
	dir := reviewRepo(t, `
[[tools.command]]
category = "review"
name = "Silent"
mode = "capture"
command = '''
true
'''
`)
	ts := serve(t, New(domain.Open(dir)))
	code, body := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Silent","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("start = %d (%v)", code, body)
	}
	events := readSSE(t, ts, body["op_id"].(string), 30*time.Second)
	last := events[len(events)-1]
	if last["ok"] != false {
		t.Fatalf("done = %v, want a failure", last)
	}
	if msg, _ := last["error"].(string); !strings.Contains(msg, "empty report") {
		t.Errorf("error = %q, want the empty-report refusal", msg)
	}
}

// An agent that hangs must not hold the single lane — and the whole UI — with
// no way out. The cancel is reported as such, not as the subprocess's signal.
func TestReviewCancel(t *testing.T) {
	dir := reviewRepo(t, `
[[tools.command]]
category = "review"
name = "Slow"
mode = "capture"
command = '''
sleep 30
'''
`)
	ts := serve(t, New(domain.Open(dir)))
	code, body := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Slow","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("start = %d (%v)", code, body)
	}
	opID := body["op_id"].(string)
	var out struct{}
	if c := postJSON(t, ts, "/api/op/"+opID+"/cancel", `{}`, "application/json", "", &out); c != http.StatusOK {
		t.Fatalf("cancel = %d", c)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	last := events[len(events)-1]
	if last["ok"] != false || last["cancelled"] != true {
		t.Fatalf("done = %v, want a cancelled failure", last)
	}
	if msg, _ := last["error"].(string); msg != "cancelled" {
		t.Errorf("error = %q, want cancelled — a killed subprocess reports its signal, not context.Canceled", msg)
	}
}

// Cancel is a review affordance only: interrupting a git operation half-way
// is a different question, so the endpoint refuses an ordinary op.
func TestCancelRefusesOrdinaryOp(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	opID := startOpJSON(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	var out struct{}
	// The lane check runs before the liveness check, so this is deterministic
	// whether or not the merge has already finished.
	if code := postJSON(t, ts, "/api/op/"+opID+"/cancel", `{}`, "application/json", "", &out); code != http.StatusConflict {
		t.Fatalf("cancel of an ordinary op = %d, want 409", code)
	}
	readSSE(t, ts, opID, 30*time.Second)
}
