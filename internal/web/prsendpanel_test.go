package web

import (
	"testing"
)

type candRowResp struct {
	ID, Kind, Severity, Path, Side, Summary, Rationale, Sync, Skip string
	Range                                                          [2]int
	Code                                                           []string
}

type candGroupResp struct {
	ID, Kind, Agent, Title, Created string
	Slot                            int
	Rows                            []candRowResp
}

type candResp struct {
	PR     int             `json:"pr"`
	Head   string          `json:"head"`
	OwnPR  bool            `json:"own_pr"`
	Groups []candGroupResp `json:"groups"`
}

// §5.1 / §5.4: the panel's list — each AI review newest first, then my
// notes, then draft replies; a row the planner would skip is listed with
// its reason; code lines ride along. Serial: sendServer.
func TestPRSendCandidatesEndpoint(t *testing.T) {
	ts, _, srv, head, _ := sendServerFull(t)
	rid := savePRReviewWeb(t, srv, `{"version":1,"summary":"Looks fine","files":[
 {"path":"pr7.txt","annotations":[{"newRange":[1,1],"summary":"check","meta":{"severity":"bug"}}]},
 {"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"not in the PR"}]}]}`)
	mine := addWebNote(t, ts, head, "pr7.txt", 1, "mine")
	var out candResp
	if code := getJSON(t, ts, "/api/pr/send/candidates?n=7", &out); code != 200 {
		t.Fatalf("code %d", code)
	}
	if out.PR != 7 || out.Head != head || len(out.Groups) != 2 {
		t.Fatalf("header/groups = %+v", out)
	}
	rev, my := out.Groups[0], out.Groups[1]
	if rev.ID != "review:"+rid || rev.Kind != "review" || rev.Agent != "claude" || rev.Title != "Looks fine" || rev.Slot == 0 || len(rev.Rows) != 2 {
		t.Fatalf("review group = %+v", rev)
	}
	// Rows sort by path then line: f.txt before pr7.txt.
	if r := rev.Rows[1]; r.ID != "review:"+rid+":0" || r.Kind != "remark" || r.Severity != "bug" || r.Path != "pr7.txt" || r.Range != [2]int{1, 1} || r.Skip != "" || len(r.Code) != 1 {
		t.Fatalf("remark row = %+v", r)
	}
	if r := rev.Rows[0]; r.ID != "review:"+rid+":1" || r.Path != "f.txt" || r.Skip == "" {
		t.Fatalf("a remark on a file the PR does not change must carry its skip reason: %+v", r)
	}
	if my.ID != "mine" || my.Kind != "mine" || len(my.Rows) != 1 || my.Rows[0].ID != mine || my.Rows[0].Kind != "note" || my.Rows[0].Code == nil {
		t.Fatalf("mine = %+v", my)
	}
	if code := getJSON(t, ts, "/api/pr/send/candidates?n=99", nil); code != 404 {
		t.Errorf("unknown PR = %d, want 404", code)
	}
}
