package web

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// readSSE GETs an op event stream and decodes every `data:` line until the
// done event, EOF, or the timeout.
func readSSE(t *testing.T, ts *httptest.Server, opID string, timeout time.Duration) []wireEvent {
	t.Helper()
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(ts.URL + "/api/op/" + opID + "/events")
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %s", ct)
	}
	var events []wireEvent
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var we wireEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &we); err != nil {
			t.Fatalf("bad SSE json %q: %v", line, err)
		}
		events = append(events, we)
		if we["type"] == "done" {
			return events
		}
	}
	t.Fatalf("stream ended without done: %v (scan err %v)", events, sc.Err())
	return nil
}

func startSwitch(t *testing.T, ts *httptest.Server, branch string) string {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	code := postJSON(t, ts, "/api/op", `{"op":"switch","branch":"`+branch+`"}`, "application/json", "", &out)
	if code != http.StatusAccepted {
		t.Fatalf("op start code = %d", code)
	}
	if out.OpID == "" {
		t.Fatal("empty op_id")
	}
	return out.OpID
}

// headBranchOf scans a commits payload for the ref decorated head:true.
// The feed is ALL-branches (subjects appear regardless of HEAD), so the
// moving HEAD decoration is the observable that proves the cached feed was
// rebuilt after a switch.
func headBranchOf(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	var commits struct {
		Rows []struct {
			Refs []struct {
				Name string `json:"name"`
				Head bool   `json:"head"`
			} `json:"refs"`
		} `json:"rows"`
	}
	if code := getJSON(t, ts, "/api/commits", &commits); code != http.StatusOK {
		t.Fatalf("commits code = %d", code)
	}
	for _, r := range commits.Rows {
		for _, ref := range r.Refs {
			if ref.Head {
				return ref.Name
			}
		}
	}
	return ""
}

func TestOpHTTPCleanSwitchAndFeedReset(t *testing.T) {
	dir := twoBranchRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	if hb := headBranchOf(t, ts); hb != "main" { // warms the feed cache too
		t.Fatalf("head branch before switch = %q, want main", hb)
	}

	opID := startSwitch(t, ts, "side")
	events := readSSE(t, ts, opID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "side" {
		t.Errorf("HEAD = %s", got)
	}
	// a stale cached feed would still decorate main as HEAD
	if hb := headBranchOf(t, ts); hb != "side" {
		t.Errorf("head branch after switch = %q, want side (feed not reset?)", hb)
	}
}

func TestOpHTTPForcedStashConflict(t *testing.T) {
	// f.txt differs on side; a dirty edit on main stashes, switches, and the
	// pop conflicts. stash-pop-conflict is NOTIFY-ONLY: decision event, then
	// done{ok:false} with no decide.
	dir := newRepoDir(t, 1) // f.txt = "content 1"
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side edit")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	opID := startSwitch(t, ts, "side")
	events := readSSE(t, ts, opID, 20*time.Second)
	dec, hasDec := findEvent(events, "decision")
	if !hasDec || dec["id"] != "stash-pop-conflict" {
		t.Fatalf("expected stash-pop-conflict decision, got %v", events)
	}
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
	if got := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "side" {
		t.Errorf("HEAD = %s, want side", got)
	}
	if out := gitRun(t, dir, "stash", "list"); out == "" {
		t.Error("stash dropped — must be preserved on conflict")
	}
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if i, ok := findFile(t, st, "f.txt"); !ok || st.Files[i].Kind != "conflicted" {
		t.Errorf("f.txt not conflicted after pop conflict: %+v", st.Files)
	}
}

func TestOpHTTPParkedDecideRoundTrip(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)
	// parked op started via the internal registry — POST /api/op only
	// accepts switch, but the decide endpoint is op-agnostic.
	run, err := srv.startOp(engine.DeleteBranch{Name: "side"})
	if err != nil {
		t.Fatal(err)
	}
	waitDecision(t, run)

	if code := postJSON(t, ts, "/api/op/"+run.id+"/decide", `{"option":"bogus"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("bogus option code = %d, want 400", code)
	}
	if code := postJSON(t, ts, "/api/op/"+run.id+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Errorf("decide code = %d, want 200", code)
	}
	events := readSSE(t, ts, run.id, 20*time.Second)
	if _, ok := findEvent(events, "decision"); !ok {
		t.Fatalf("no decision in %v", events)
	}
	if code := postJSON(t, ts, "/api/op/"+run.id+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusConflict {
		t.Errorf("decide after done code = %d, want 409", code)
	}
	if out := gitRun(t, dir, "branch", "--list", "side"); !strings.Contains(out, "side") {
		t.Error("side deleted despite abort")
	}
}

func TestOpHTTPBusyAndValidation(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	run, err := srv.startOp(engine.DeleteBranch{Name: "side"})
	if err != nil {
		t.Fatal(err)
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op", `{"op":"switch","branch":"side"}`, "application/json", "", nil); code != http.StatusConflict {
		t.Errorf("busy code = %d, want 409", code)
	}
	if err := run.decide("abort"); err != nil {
		t.Fatal(err)
	}
	readSSE(t, ts, run.id, 20*time.Second)

	cases := []struct {
		body, name string
		want       int
	}{
		{`{"op":"pull","branch":"side"}`, "unknown op", http.StatusBadRequest},
		{`{"op":"switch","branch":""}`, "empty branch", http.StatusBadRequest},
		{`{"op":"switch","branch":"--evil"}`, "argv-unsafe branch", http.StatusBadRequest},
		{`{`, "bad json", http.StatusBadRequest},
	}
	for _, c := range cases {
		if code := postJSON(t, ts, "/api/op", c.body, "application/json", "", nil); code != c.want {
			t.Errorf("%s: code = %d, want %d", c.name, code, c.want)
		}
	}
	if code := postJSON(t, ts, "/api/op/nope/decide", `{"option":"x"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("decide unknown id code = %d, want 404", code)
	}
	resp, err := http.Get(ts.URL + "/api/op/nope/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("events unknown id code = %d, want 404", resp.StatusCode)
	}
	// write guard covers the op endpoints
	if code := postJSON(t, ts, "/api/op", `{"op":"switch","branch":"side"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("op without json content type = %d, want 415", code)
	}
}
