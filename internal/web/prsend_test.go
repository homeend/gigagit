package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// writerForge is fakeForge + a recording forge.Writer + Snapshot: the web's
// send tests never reach a real forge.
type writerForge struct {
	*fakeForge
	wmu                    sync.Mutex
	writes                 []string
	failSubmit, failDelete bool // a send that breaks half-way (an interrupted send)
}

func (f *writerForge) Snapshot(ctx context.Context, n int) (forge.Snapshot, error) {
	return snapForge{f.fakeForge}.Snapshot(ctx, n)
}

func (f *writerForge) log(s string) {
	f.wmu.Lock()
	f.writes = append(f.writes, s)
	f.wmu.Unlock()
}

func (f *writerForge) writeLog() string {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	return strings.Join(f.writes, "\n")
}

func (f *writerForge) StartReview(_ context.Context, prID, _ string) (string, error) {
	f.log("StartReview " + prID)
	return "PRR_1", nil
}

func (f *writerForge) AddThread(_ context.Context, review string, t forge.Thread) (forge.ThreadRef, error) {
	f.log(fmt.Sprintf("AddThread %s %s:%d", review, t.Path, t.Line))
	return forge.ThreadRef{ID: "PRRT_1", CommentID: "PRRC_1"}, nil
}

func (f *writerForge) Reply(_ context.Context, _, thread, _ string) (forge.CommentRef, error) {
	f.log("Reply " + thread)
	return forge.CommentRef{ID: "PRRC_2"}, nil
}

func (f *writerForge) SubmitReview(_ context.Context, _ string, ev forge.Event, body string) error {
	f.log(fmt.Sprintf("SubmitReview %s %q", ev, body))
	if f.failSubmit {
		return fmt.Errorf("HTTP 502: Bad Gateway")
	}
	return nil
}

func (f *writerForge) DeletePendingReview(_ context.Context, review string) error {
	f.log("DeletePendingReview " + review)
	if f.failDelete {
		return fmt.Errorf("HTTP 502: Bad Gateway")
	}
	return nil
}

func (f *writerForge) Resolve(_ context.Context, thread string) error {
	f.log("Resolve " + thread)
	return nil
}

func (f *writerForge) Unresolve(_ context.Context, thread string) error {
	f.log("Unresolve " + thread)
	return nil
}

// sendServer is a fetched PR #7 (node id PR_7, pr7.txt added) on a writable
// fake forge, with notes on (TestMain turns them off package-wide).
func sendServer(t *testing.T) (*httptest.Server, *writerForge, string) {
	t.Helper()
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA, pr.NodeID = head, "PR_7"
	wf := &writerForge{fakeForge: &fakeForge{open: []model.PullRequest{pr}, baseURL: bare, comments: prThreads()}}
	ts, srv := prServe(t, dir, wf)
	srv.service().UseNotesDir(t.TempDir())
	waitPRsLoaded(t, ts)
	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch: %v", done)
	}
	return ts, wf, head
}

// addWebNote writes a note on the PR head's path:line the way the page does.
func addWebNote(t *testing.T, ts *httptest.Server, head, path string, line int, summary string) string {
	t.Helper()
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":%q,"rev":%q,"state":"commit","side":"new","line":%d,"summary":%q}`, path, head, line, summary))
	id, _ := out["id"].(string)
	if code != 200 || id == "" {
		t.Fatalf("add note = %d %v", code, out)
	}
	return id
}

// Serial: sendServer → prFixture isolates XDG state with t.Setenv.
func TestPRNotesCountsCarryGroupSlots(t *testing.T) {
	ts, _, head := sendServer(t)
	addWebNote(t, ts, head, "pr7.txt", 1, "mine")
	var out struct {
		Groups map[string][]int `json:"groups"`
		Notes  []wireNote       `json:"notes"`
	}
	if code := getJSON(t, ts, "/api/pr/notes?n=7&path=pr7.txt", &out); code != 200 {
		t.Fatalf("code %d", code)
	}
	if got := out.Groups["pr7.txt"]; len(got) == 0 || got[0] != 5 {
		t.Fatalf("groups = %v (want my draft review's slot 5 first)", out.Groups)
	}
	for _, n := range out.Notes {
		if n.Summary == "mine" && n.GroupSlot != 5 {
			t.Fatalf("the note's group_slot = %d", n.GroupSlot)
		}
	}
}

// followDecide reads op id's events, answers its forge.send decision with
// option, and returns every event through done.
func followDecide(t *testing.T, ts *httptest.Server, id, option string) []wireEvent {
	t.Helper()
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(ts.URL + "/api/op/" + id + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var evs []wireEvent
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var we wireEvent
		if err := json.Unmarshal([]byte(line), &we); err != nil {
			t.Fatal(err)
		}
		evs = append(evs, we)
		switch we["type"] {
		case "decision":
			if code, out := postJSONAny(t, ts, "/api/op/"+id+"/decide", `{"option":"`+option+`"}`); code != 200 {
				t.Fatalf("decide = %d %v", code, out)
			}
		case "done":
			return evs
		}
	}
	t.Fatalf("no done: %v", evs)
	return nil
}

// Serial: sendServer.
func TestWebSendsOneNoteBehindTheConfirm(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "rename this")
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if code != 202 {
		t.Fatalf("start = %d %v", code, out)
	}
	plan, _ := json.Marshal(out["plan"])
	if strings.Contains(string(plan), head) || strings.Contains(string(plan), "PR_7") || strings.Contains(string(plan), id) {
		t.Fatalf("the plan leaks a sha, a node id or a key: %s", plan)
	}
	if !strings.Contains(string(plan), "rename this") || !strings.Contains(string(plan), `"target"`) {
		t.Fatalf("plan = %s", plan)
	}
	if wf.writeLog() != "" {
		t.Fatal("nothing may be written before the confirm is answered")
	}
	evs := followDecide(t, ts, out["op_id"].(string), "send")
	done, _ := findEvent(evs, "done")
	if done["ok"] != true || !strings.Contains(wf.writeLog(), "SubmitReview COMMENT") {
		t.Fatalf("done %v, writes %s", done, wf.writeLog())
	}
}

// Serial: sendServer.
func TestWebSendAbortPostsNothing(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if code != 202 {
		t.Fatalf("start = %d %v", code, out)
	}
	followDecide(t, ts, out["op_id"].(string), "abort")
	if w := wf.writeLog(); w != "" {
		t.Fatalf("an aborted send wrote: %s", w)
	}
}

// Review Focus 3: a forged wire value is refused before anything is planned.
// Serial: sendServer.
func TestWebSendRefusesForgedValues(t *testing.T) {
	ts, wf, head := sendServer(t)
	addWebNote(t, ts, head, "pr7.txt", 1, "x")
	big := strings.Repeat("a", 70<<10)
	for _, tc := range []struct {
		q, body string
		code    int
	}{
		{"n=0", `{"kind":"verdict"}`, 400},
		{"n=99", `{"kind":"verdict"}`, 404},
		{"n=7", `{"kind":"post-anything"}`, 400},
		{"n=7", `{"kind":"notes","ids":[]}`, 400},
		{"n=7", `{"kind":"notes","ids":["` + head + `"]}`, 400},
		{"n=7", `{"kind":"notes","ids":["../x"]}`, 400},
		{"n=7", `{"kind":"notes","ids":["forge:C1"]}`, 400}, // a GitHub thread is not a note to send
		{"n=7", `{"kind":"resolve","ids":["forge:NOPE"]}`, 400},
		{"n=7", `{"kind":"group","group":"review:not-listed"}`, 400},
		{"n=7", `{"kind":"verdict","body":"` + big + `"}`, 400},
	} {
		if code, out := postJSONAny(t, ts, "/api/pr/send?"+tc.q, tc.body); code != tc.code {
			t.Errorf("%s %.40s = %d %v, want %d", tc.q, tc.body, code, out, tc.code)
		}
	}
	if w := wf.writeLog(); w != "" {
		t.Fatalf("a refused request wrote: %s", w)
	}
}

// Review Focus 2: the head moved on GitHub — refused, nothing planned.
// Serial: sendServer.
func TestWebSendRefusesAMovedHead(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	wf.mu.Lock()
	wf.open[0].HeadSHA = strings.Repeat("e", 40)
	wf.mu.Unlock()
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "new commits") || out["code"] != "head_moved" {
		t.Fatalf("= %d %v (want 409, code head_moved: the page follows the head)", code, out)
	}
	if w := wf.writeLog(); w != "" {
		t.Fatalf("a refused send wrote: %s", w)
	}
}

// The page follows a moved head when a send is refused for it (final review
// Important 1): prsend.js hands the refusal to prs.js's followMovedHead.
func TestAHeadMovedRefusalFollowsTheHead(t *testing.T) {
	t.Parallel()
	read := func(f string) string {
		b, err := os.ReadFile(filepath.Join("static", f))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if js := read("prsend.js"); !strings.Contains(js, `err.data && err.data.code === "head_moved"`) || !strings.Contains(js, "export function onHeadMoved(") {
		t.Error("prsend.js does not route a head_moved refusal")
	}
	if !strings.Contains(read("prs.js"), "onHeadMoved((n) => followMovedHead(n));") {
		t.Error("prs.js does not follow the head on a refused send")
	}
}

// Serial: sendServer.
func TestWebSendGroupsListsMine(t *testing.T) {
	ts, _, head := sendServer(t)
	addWebNote(t, ts, head, "pr7.txt", 1, "x")
	var out struct {
		Groups []struct {
			ID    string `json:"id"`
			Count int    `json:"count"`
			Slot  int    `json:"slot"`
		} `json:"groups"`
	}
	if code := getJSON(t, ts, "/api/pr/send/groups?n=7", &out); code != 200 || len(out.Groups) != 1 ||
		out.Groups[0].ID != "mine" || out.Groups[0].Count != 1 || out.Groups[0].Slot != 5 {
		t.Fatalf("= %d %+v", code, out)
	}
}

// Review Focus 1: one op at a time — a second send while one is parked on
// its confirm is refused. Serial: sendServer.
func TestWebSendWhileAnOpRunsIs409(t *testing.T) {
	ts, _, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	_, first := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"verdict"}`); code != 409 {
		t.Fatalf("a second send while one is parked = %d %v", code, out)
	}
	followDecide(t, ts, first["op_id"].(string), "abort")
}

// Serial: sendServer. The send endpoint is a guarded POST.
func TestWebSendIsWriteGuarded(t *testing.T) {
	ts, _, _ := sendServer(t)
	resp, err := http.Post(ts.URL+"/api/pr/send?n=7", "text/plain", strings.NewReader(`{"kind":"verdict"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 415 {
		t.Fatalf("a non-JSON post = %d", resp.StatusCode)
	}
}

// W6: a send that broke half-way (submit failed, the pending review could
// not be deleted) is named by the refresh answers, so the page can offer to
// finish or discard it. Serial: sendServer.
func TestRefreshAnswersCarryAnInterruptedSend(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	wf.wmu.Lock()
	wf.failSubmit, wf.failDelete = true, true
	wf.wmu.Unlock()
	_, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if done, _ := findEvent(followDecide(t, ts, out["op_id"].(string), "send"), "done"); done["ok"] == true {
		t.Fatal("the send was meant to fail")
	}
	wf.mu.Lock()
	wf.open[0].ViewerPendingReview = "PRR_1" // GitHub still holds gg's pending review
	wf.mu.Unlock()
	type interrupted struct {
		Count  int  `json:"count"`
		Joined bool `json:"joined"`
	}
	var rv struct {
		Interrupted *interrupted `json:"interrupted"`
	}
	if code := postJSON(t, ts, "/api/pr/revalidate?n=7", `{}`, "application/json", "", &rv); code != 200 || rv.Interrupted == nil || rv.Interrupted.Count != 1 {
		t.Fatalf("revalidate = %d %+v", code, rv.Interrupted)
	}
	var cr struct {
		Interrupted *interrupted `json:"interrupted"`
	}
	if code := postJSON(t, ts, "/api/pr/comments/refresh?n=7", `{}`, "application/json", "", &cr); code != 200 || cr.Interrupted == nil {
		t.Fatalf("comments refresh = %d %+v", code, cr.Interrupted)
	}
	// Nothing pending on GitHub: no offer.
	wf.mu.Lock()
	wf.open[0].ViewerPendingReview = ""
	wf.mu.Unlock()
	var none struct {
		Interrupted *interrupted `json:"interrupted"`
	}
	if code := postJSON(t, ts, "/api/pr/revalidate?n=7", `{}`, "application/json", "", &none); code != 200 || none.Interrupted != nil {
		t.Fatalf("no pending review, yet = %d %+v", code, none.Interrupted)
	}
}
