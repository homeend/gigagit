package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// writerForge is fakeForge + a recording forge.Writer + Snapshot: the web's
// send tests never reach a real forge.
type writerForge struct {
	*fakeForge
	wmu                    sync.Mutex
	writes                 []string
	failSubmit, failDelete bool          // a send that breaks half-way (an interrupted send)
	snapGate               chan struct{} // when non-nil, Snapshot waits for it or its ctx
}

func (f *writerForge) Snapshot(ctx context.Context, n int) (forge.Snapshot, error) {
	f.wmu.Lock()
	g := f.snapGate
	f.wmu.Unlock()
	if g != nil {
		select {
		case <-g:
		case <-ctx.Done():
			return forge.Snapshot{}, ctx.Err()
		}
	}
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
	return sendServerWith(t)
}

// sendServerWith is sendServer listing more open pull requests (never fetched).
func sendServerWith(t *testing.T, more ...model.PullRequest) (*httptest.Server, *writerForge, string) {
	t.Helper()
	ts, wf, _, head, _ := sendServerFull(t, more...)
	return ts, wf, head
}

// sendServerFull is sendServerWith that also hands back the server and the
// work dir.
func sendServerFull(t *testing.T, more ...model.PullRequest) (*httptest.Server, *writerForge, *Server, string, string) {
	t.Helper()
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA, pr.NodeID = head, "PR_7"
	cs := prThreads()
	for i := range cs { // the threads a send resolves and replies to
		switch cs[i].ID {
		case "C1", "C2":
			cs[i].ThreadID = "PRRT_c1"
		case "C3":
			cs[i].ThreadID = "PRRT_c3"
		}
	}
	wf := &writerForge{fakeForge: &fakeForge{open: []model.PullRequest{pr}, baseURL: bare, comments: cs}}
	wf.open = append(wf.open, more...)
	ts, srv := prServe(t, dir, wf)
	srv.service().UseNotesDir(t.TempDir())
	waitPRsLoaded(t, ts)
	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch: %v", done)
	}
	return ts, wf, srv, head, dir
}

// addWebNote writes a note on the PR head's path:line the way the page does.
func addWebNote(t *testing.T, ts *httptest.Server, head, path string, line int, summary string) string {
	t.Helper()
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":%q,"rev":%q,"state":"commit","side":"new","line":%d,"summary":%q,"pr":7}`, path, head, line, summary))
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
		{"n=7", `{"kind":"group","group":"mine"}`, 400},              // the kind is gone (W6)
		{"n=7", `{"kind":"notes","ids":["review:deadbeef:0"]}`, 400}, // a remark of a review the PR does not list
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

// §5.4: the panel's send is kind notes with a verdict and the body taken
// from a review (body_from): one review op — start, the remark, submit
// with that body. Serial: sendServer.
func TestWebSendNotesWithVerdictAndBodyFrom(t *testing.T) {
	ts, wf, srv, head, _ := sendServerFull(t)
	rid := savePRReviewWeb(t, srv, prReviewDoc)
	mine := addWebNote(t, ts, head, "pr7.txt", 1, "mine too")
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["review:`+rid+`:0","`+mine+`"],"verdict":true,"body_from":"`+rid+`"}`)
	if code != 202 {
		t.Fatalf("start = %d %v", code, out)
	}
	plan, _ := json.Marshal(out["plan"])
	if !strings.Contains(string(plan), `"verdict":true`) || !strings.Contains(string(plan), "pr review") {
		t.Fatalf("plan = %s (want the verdict and the review's summary as the body)", plan)
	}
	evs := followDecide(t, ts, out["op_id"].(string), "comment")
	done, _ := findEvent(evs, "done")
	if done["ok"] != true || !strings.Contains(wf.writeLog(), "SubmitReview COMMENT") {
		t.Fatalf("done %v, writes %s", done, wf.writeLog())
	}
	// A body_from the PR does not own is refused.
	if code, _ := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+mine+`"],"body_from":"deadbeef"}`); code != 400 && code != 404 {
		t.Fatalf("a foreign body_from = %d, want a refusal", code)
	}
}

// The groups route left with kind group (W6).
func TestWebSendGroupsRouteIsGone(t *testing.T) {
	ts, _, _ := sendServer(t)
	if code := getJSON(t, ts, "/api/pr/send/groups?n=7", nil); code != 404 && code != 405 {
		t.Fatalf("/api/pr/send/groups = %d, want gone", code)
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

// sendKind posts one send, answers its confirm with "send", and returns the
// forge's writes.
func sendKind(t *testing.T, ts *httptest.Server, wf *writerForge, body string) string {
	t.Helper()
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", body)
	if code != 202 {
		t.Fatalf("start %s = %d %v", body, code, out)
	}
	evs := followDecide(t, ts, out["op_id"].(string), "send")
	if done, _ := findEvent(evs, "done"); done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	return wf.writeLog()
}

// Serial: sendServer.
func TestWebResolvesAndReopensAThread(t *testing.T) {
	ts, wf, _ := sendServer(t)
	if w := sendKind(t, ts, wf, `{"kind":"resolve","ids":["forge:C1"]}`); !strings.Contains(w, "Resolve PRRT_c1") {
		t.Fatalf("writes %s", w)
	}
	if w := sendKind(t, ts, wf, `{"kind":"unresolve","ids":["forge:C3"]}`); !strings.Contains(w, "Unresolve PRRT_c3") {
		t.Fatalf("writes %s", w)
	}
}

// Serial: sendServer.
func TestWebSendsADraftReply(t *testing.T) {
	ts, wf, _ := sendServer(t)
	// The page reads the PR's threads before anyone replies.
	if code, out := postJSONAny(t, ts, "/api/pr/comments/refresh?n=7", `{}`); code != 200 {
		t.Fatalf("refresh = %d %v", code, out)
	}
	code, out := postJSONAny(t, ts, "/api/notes/reply", `{"id":"forge:C1","summary":"done in the next push"}`)
	id, _ := out["id"].(string)
	if code != 200 || id == "" {
		t.Fatalf("reply = %d %v", code, out)
	}
	if w := sendKind(t, ts, wf, `{"kind":"notes","ids":["`+id+`"]}`); !strings.Contains(w, "Reply PRRT_c1") {
		t.Fatalf("writes %s", w)
	}
}

// Review Focus 2: a forge that never answers costs the POST its budget, not
// a hung socket; nothing is written. Serial: sendServer + a package var.
func TestWebSendPlanHasAForgeBudget(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	prev := prSendBudget
	prSendBudget = 300 * time.Millisecond
	defer func() { prSendBudget = prev }()
	wf.wmu.Lock()
	wf.snapGate = make(chan struct{}) // never closed
	wf.wmu.Unlock()
	start := time.Now()
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if code != http.StatusGatewayTimeout || time.Since(start) > 5*time.Second {
		t.Fatalf("= %d %v after %v (want 504 within the budget)", code, out, time.Since(start))
	}
	if w := wf.writeLog(); w != "" {
		t.Fatalf("wrote %s", w)
	}
}

func TestPRSendLookupStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w: #8's diff is not available here", domain.ErrSendRequest), 422},
		{context.DeadlineExceeded, 504},
		{errors.Join(domain.ErrForgeUnavailable, errors.New("gh: not logged in")), 502},
		{errors.New("disk on fire"), 422},
	} {
		if got := prSendLookupStatus(tc.err); got != tc.want {
			t.Errorf("%v → %d, want %d", tc.err, got, tc.want)
		}
	}
}

// Item 9: a PR whose diff is not fetched is not the request's fault: 422.
// Serial: sendServer.
func TestWebSendOnAnUnfetchedPRIs422(t *testing.T) {
	pr8 := openPR(8, "Another")
	pr8.HeadSHA = strings.Repeat("d", 40)
	ts, wf, _ := sendServerWith(t, pr8)
	if code, out := postJSONAny(t, ts, "/api/pr/send?n=8", `{"kind":"notes","ids":["x"]}`); code != 422 {
		t.Fatalf("= %d %v", code, out)
	}
	if w := wf.writeLog(); w != "" {
		t.Fatalf("wrote %s", w)
	}
}

// Final review I2: a forge that cannot be detected answers 502 whichever
// kind reached it (verdict/finish/discard skip the lookup and fail in
// planning).
func TestSendErrStatusForgeUnavailable(t *testing.T) {
	t.Parallel()
	err := errors.Join(domain.ErrForgeUnavailable, errors.New("gh: not logged in"))
	if got := sendErrStatus(err); got != http.StatusBadGateway {
		t.Fatalf("sendErrStatus = %d, want 502", got)
	}
}
