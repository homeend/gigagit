package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// The bar is wired: the slot sits after refresh, and the page loads the module.
func TestTopBarWiring(t *testing.T) {
	t.Parallel()
	html := readStatic(t, "index.html")
	if r, c, w := strings.Index(html, `id="refresh-btn"`), strings.Index(html, `id="top-custom"`), strings.Index(html, `id="repo-worktree"`); r < 0 || c < r || w < c {
		t.Errorf("the custom slot must sit between refresh and the path: %d %d %d", r, c, w)
	}
	if !strings.Contains(readStatic(t, "app.js"), `from "./topbar.js"`) {
		t.Error("app.js does not load topbar.js")
	}
	if !strings.Contains(readStatic(t, "polish.css"), "#top-sep") {
		t.Error("polish.css has no separator rule")
	}
}

type fitResult struct {
	Shown   int  `json:"shown"`
	CutPath bool `json:"cutPath"`
}

func TestToolbarFit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args string
		want fitResult
	}{
		{"all fit, no room kept for the more button", `{room:400,fixed:100,sep:10,more:30,path:100,buttons:[90,100]}`, fitResult{2, false}},
		{"one too many", `{room:399,fixed:100,sep:10,more:30,path:100,buttons:[90,100]}`, fitResult{1, false}},
		{"drops until the more button fits too", `{room:329,fixed:100,sep:10,more:30,path:100,buttons:[90,100]}`, fitResult{0, false}},
		{"a wide middle button keeps its place: later narrow ones stay behind it", `{room:400,fixed:100,sep:10,more:30,path:100,buttons:[50,500,10]}`, fitResult{1, false}},
		{"nothing fits but the path is whole", `{room:240,fixed:100,sep:10,more:30,path:100,buttons:[90]}`, fitResult{0, false}},
		{"path cut only with no button left", `{room:239,fixed:100,sep:10,more:30,path:100,buttons:[90]}`, fitResult{0, true}},
		{"no buttons: the row is untouched even when too wide", `{room:10,fixed:100,sep:10,more:30,path:100,buttons:[]}`, fitResult{0, false}},
	}
	for _, c := range cases {
		var got fitResult
		pureJS(t, "toolbarfit.js", "// --- toolbar fit (pure; guarded against Go) ---", "// --- end toolbar fit ---", "fitToolbar("+c.args+")", &got)
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestResolveToolbar(t *testing.T) {
	t.Parallel()
	var got []struct {
		Label  string `json:"label"`
		Status string `json:"status"`
	}
	expr := `resolveToolbar(["a","pull","gone","b","a","push","refresh","1","2","3","4","5","6"],
		[{label:"a",available:true},{label:"b",available:false},{label:"pull",available:true}])`
	pureJS(t, "toolbarfit.js", "// --- toolbar fit (pure; guarded against Go) ---", "// --- end toolbar fit ---", expr, &got)
	want := []string{"a:ok", "pull:unknown", "gone:unknown", "b:unavailable", "push:unknown", "refresh:unknown", "1:unknown", "2:unknown", "3:unknown", "4:unknown"}
	if len(got) != len(want) {
		raw, _ := json.Marshal(got)
		t.Fatalf("got %s, want %d rows", raw, len(want))
	}
	for i, w := range want {
		if got[i].Label+":"+got[i].Status != w {
			t.Errorf("row %d = %s:%s, want %s", i, got[i].Label, got[i].Status, w)
		}
	}
}
