package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/steer"
)

const webMemID = "20261102T031500-a1b2"

func TestWebMemdocVerbs(t *testing.T) {
	isolateGlobal(t)
	s := newSteerServer(t)
	ts := serve(t, s)
	plain := true
	wt := s.service().Root()
	rep, err := steer.PostHTTPReply(ts.URL, steer.Command{ID: "c", Cmd: "memdoc_add", FileID: webMemID, Title: "T", Text: "<b>x</b>", Worktree: wt, Plain: &plain})
	if err != nil || !rep.OK {
		t.Fatalf("add: %+v %v", rep, err)
	}
	rep, err = steer.PostHTTPReply(ts.URL, steer.Command{ID: "d", Cmd: "memdoc_show", FileID: webMemID, Worktree: wt})
	if err != nil || !rep.OK || len(rep.Overviews) != 1 || rep.Overviews[0].Text != "<b>x</b>" {
		t.Fatalf("show: %+v %v", rep, err)
	}
}

func postMemdoc(t *testing.T, url, title, text string) (int, map[string]string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": title, "text": text})
	req, _ := http.NewRequest(http.MethodPost, url+"/api/session-overview/memory", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", url)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestWebCreateRoute(t *testing.T) {
	isolateGlobal(t)
	s, root := noteSrv(t)
	ts := serve(t, s)
	code, out := postMemdoc(t, ts.URL, "T", "body")
	if code != http.StatusOK || !agentdocs.ValidID(out["id"]) || !strings.HasPrefix(out["link"], "gg://") {
		t.Fatalf("%d %v", code, out)
	}
	o, ok := s.docs.OverviewIn(root, out["id"])
	if !ok || !o.Memory || !o.Plain {
		t.Fatalf("doc = %+v", o)
	}
	if code, _ := postMemdoc(t, ts.URL, "", "body"); code != http.StatusUnprocessableEntity {
		t.Fatalf("no title: %d", code)
	}
	if code, _ := postMemdoc(t, ts.URL, "T", strings.Repeat("x", agentdocs.MaxOverviewBytes+1)); code != http.StatusUnprocessableEntity {
		t.Fatalf("too long: %d", code)
	}
	if _, ok := s.ofs.resolve(s.service().Root(), out["id"], ""); !ok {
		t.Fatal("not listed")
	}
}

func TestWebOpenFilesFlagsAndLink(t *testing.T) {
	isolateGlobal(t)
	s, root := noteSrv(t)
	ts := serve(t, s)
	wt := s.service().Root()
	if _, err := s.docs.AddMemoryOverview(root, wt, webMemID, "T", "body", "", true); err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	var list struct {
		Files []struct {
			ID     string `json:"id"`
			Memory bool   `json:"memory"`
			Plain  bool   `json:"plain"`
		} `json:"files"`
	}
	if code := getJSON(t, ts, "/api/open-files", &list); code != http.StatusOK || len(list.Files) != 1 || !list.Files[0].Memory || !list.Files[0].Plain {
		t.Fatalf("%d %+v", code, list)
	}
	var doc struct {
		Plain  bool   `json:"plain"`
		Memory bool   `json:"memory"`
		Text   string `json:"text"`
	}
	if code := getJSON(t, ts, "/api/overview?id="+webMemID, &doc); code != http.StatusOK || !doc.Plain || !doc.Memory || doc.Text != "body" {
		t.Fatalf("%d %+v", code, doc)
	}
	var link map[string]string
	if code := getJSON(t, ts, "/api/session-overview/link?id="+webMemID, &link); code != http.StatusOK || !strings.Contains(link["link"], "?overview="+webMemID) {
		t.Fatalf("%d %v", code, link)
	}
	if code := getJSON(t, ts, "/api/overview-export?id="+webMemID, nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("export: %d", code)
	}
}
