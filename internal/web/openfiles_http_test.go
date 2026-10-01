package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

type ofAns struct {
	File    *steer.OpenFile  `json:"file"`
	Evicted string           `json:"evicted"`
	Cap     int              `json:"cap"`
	Files   []steer.OpenFile `json:"files"`
}

func postOpenFiles(t *testing.T, ts *httptest.Server, body string) (int, ofAns) {
	t.Helper()
	var a ofAns
	code := postJSON(t, ts, "/api/open-files", body, "application/json", "", &a)
	return code, a
}

func TestOpenFilesHTTPOpenListClose(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	code, a := postOpenFiles(t, ts, `{"op":"open","src":"worktree","path":"f.txt","line":3,"tab":"t1"}`)
	if code != http.StatusOK || a.File == nil || a.File.ID != "f1" || a.File.State != "shown" || a.File.Line != 3 || len(a.Files) != 1 {
		t.Fatalf("open: %d %+v", code, a)
	}
	var l struct{ Files []steer.OpenFile }
	if getJSON(t, ts, "/api/open-files", &l); len(l.Files) != 1 || l.Files[0].Path != "f.txt" {
		t.Fatalf("list: %+v", l)
	}
	if code, a = postOpenFiles(t, ts, `{"op":"background","id":"f1","tab":"t1","line":9}`); code != 200 || a.Files[0].State != "background" || a.Files[0].Line != 9 {
		t.Fatalf("background: %d %+v", code, a)
	}
	if code, a = postOpenFiles(t, ts, `{"op":"focus","id":"f1","tab":"t1"}`); code != 200 || a.File.State != "shown" {
		t.Fatalf("focus: %d %+v", code, a)
	}
	if code, _ = postOpenFiles(t, ts, `{"op":"cursor","id":"f1","line":4}`); code != 200 {
		t.Fatalf("cursor: %d", code)
	}
	if code, a = postOpenFiles(t, ts, `{"op":"close","id":"f1","tab":"t1"}`); code != 200 || len(a.Files) != 0 {
		t.Fatalf("close: %d %+v", code, a)
	}
}

func TestOpenFilesHTTPRefusals(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"op":"open","src":"worktree","path":"../x","tab":"t1"}`, 400},
		{`{"op":"open","src":"worktree","path":"/etc/passwd","tab":"t1"}`, 400},
		{`{"op":"open","src":"worktree","path":"-x","tab":"t1"}`, 400},
		{`{"op":"open","src":"commit","path":"f.txt","tab":"t1"}`, 400},
		{`{"op":"open","src":"nope","path":"f.txt","tab":"t1"}`, 400},
		{`{"op":"open","src":"worktree","path":"f.txt","tab":"bad tab!"}`, 400},
		{`{"op":"launch"}`, 400},
		{`{"op":"focus","id":"f99","tab":"t1"}`, 404},
		{`{"op":"close","id":"f99","tab":"t1"}`, 404},
	} {
		if code, _ := postOpenFiles(t, ts, c.body); code != c.want {
			t.Errorf("%s: %d, want %d", c.body, code, c.want)
		}
	}
	// The write guard: a form post is refused.
	if code := postJSON(t, ts, "/api/open-files", `{"op":"open"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Fatalf("write guard: %d", code)
	}
}

func TestOpenFilesHTTPEvictionNamed(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for i := 0; i < maxOpenFiles; i++ {
		postOpenFiles(t, ts, `{"op":"open","src":"commit","rev":"HEAD","path":"p`+strconv.Itoa(i)+`.txt"}`)
	}
	_, a := postOpenFiles(t, ts, `{"op":"open","src":"commit","rev":"HEAD","path":"last.txt"}`)
	if a.Evicted != "p0.txt" || a.Cap != maxOpenFiles {
		t.Fatalf("evicted %q cap %d", a.Evicted, a.Cap)
	}
}

// eventsFor opens /api/events as tab and returns a reader of its data messages.
func eventsFor(t *testing.T, ts *httptest.Server, tab string) (next func() liveMsg, stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events?tab="+tab, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	msgs := make(chan liveMsg, 32)
	go func() {
		defer close(msgs)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				var m liveMsg
				if json.Unmarshal([]byte(strings.TrimPrefix(l, "data: ")), &m) == nil {
					msgs <- m
				}
			}
		}
	}()
	next = func() liveMsg {
		select {
		case m := <-msgs:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no SSE message")
		}
		return liveMsg{}
	}
	return next, func() { cancel(); resp.Body.Close() }
}

func TestOpenFilesBroadcastAndStreamScope(t *testing.T) {
	isolateGlobal(t)
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	next, stop := eventsFor(t, ts, "t1")
	if m := next(); m.Reason != "hello" {
		t.Fatalf("hello first: %+v", m)
	}
	postOpenFiles(t, ts, `{"op":"open","src":"worktree","path":"f.txt","tab":"t1"}`)
	m := next()
	if m.Reason != "open_files" || len(m.Files) != 1 || m.Files[0].State != "shown" {
		t.Fatalf("open_files: %+v", m)
	}
	// A second tab's stream watches the first tab's stream end.
	next2, stop2 := eventsFor(t, ts, "t2")
	defer stop2()
	next2() // hello
	stop()
	m = next2()
	if m.Reason != "open_files" || m.Files[0].State != "background" {
		t.Fatalf("a closed stream shows nothing: %+v", m)
	}
}

func TestOpenFilesStreamRefcount(t *testing.T) {
	isolateGlobal(t)
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next()
	postOpenFiles(t, ts, `{"op":"open","src":"worktree","path":"f.txt","tab":"t1"}`)
	next()
	// A hub swap (re-root / settings write) ends the stream; the page
	// reconnects and re-reports shown after the hello (ruling L2).
	srv.restartLive(context.Background())
	nextB, stopB := eventsFor(t, ts, "t1")
	defer stopB()
	for m := nextB(); m.Reason != "hello"; m = nextB() {
	}
	if code, _ := postOpenFiles(t, ts, `{"op":"shown","id":"f1","tab":"t1"}`); code != 200 {
		t.Fatalf("shown: %d", code)
	}
	var l struct{ Files []steer.OpenFile }
	getJSON(t, ts, "/api/open-files", &l)
	if l.Files[0].State != "shown" {
		t.Fatalf("after the reconnect: %+v", l.Files)
	}
}
