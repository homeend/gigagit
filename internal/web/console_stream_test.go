package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func runs(texts ...string) domain.ScreenRunRow {
	r := domain.ScreenRunRow{Runs: []domain.ScreenRun{}}
	for _, t := range texts {
		r.Runs = append(r.Runs, domain.ScreenRun{Text: t})
	}
	return r
}

func TestDiffFrameSendsOnlyChangedRowsAndFullOnResize(t *testing.T) {
	t.Parallel()
	a := domain.ScreenRuns{Cols: 10, Rows: 3, Lines: []domain.ScreenRunRow{runs("a"), runs("b"), runs()}}
	f := diffFrame(nil, a)
	if !f.Full || len(f.Lines) != 3 || f.Cols != 10 {
		t.Fatalf("first frame = %+v", f)
	}
	b := a
	b.Lines = []domain.ScreenRunRow{runs("a"), runs("B"), runs()}
	b.CursorX = 4
	f = diffFrame(&a, b)
	if f.Full || len(f.Lines) != 1 || f.Lines[0].Y != 1 || f.Lines[0].Runs[0].Text != "B" || f.CX != 4 {
		t.Fatalf("partial = %+v", f)
	}
	c := b
	c.Cols = 12
	if f = diffFrame(&b, c); !f.Full || len(f.Lines) != 3 {
		t.Fatalf("resize frame = %+v", f)
	}
	if f = diffFrame(&c, c); f.Full || len(f.Lines) != 0 {
		t.Fatalf("same frame = %+v", f)
	}
}

type sseEvent struct {
	Name string
	Data string
}

// readConsoleSSE reads up to n events (or until timeout, or a gone event)
// from the session stream.
func readConsoleSSE(t *testing.T, ts *httptest.Server, id string, n int, timeout time.Duration) []sseEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/session-screen?id="+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for len(out) < n && sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = strings.TrimPrefix(line, "data: ")
		case line == "" && cur.Name != "":
			out = append(out, cur)
			if cur.Name == "gone" {
				return out
			}
			cur = sseEvent{}
		}
	}
	return out
}

func TestSessionScreenHelloThenFramesThenExited(t *testing.T) {
	s := testSession(t, `printf 'FIRST'; sleep 1; printf ' SECOND'; sleep 1; exit 7`)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	evs := readConsoleSSE(t, ts, string(s.Info().ID), 6, 8*time.Second)
	if len(evs) == 0 || evs[0].Name != "hello" {
		t.Fatalf("events %+v", evs)
	}
	var hello struct {
		Frame   frameWire `json:"frame"`
		Palette []string  `json:"palette"`
	}
	if err := json.Unmarshal([]byte(evs[0].Data), &hello); err != nil || !hello.Frame.Full || len(hello.Palette) != 16 || hello.Frame.Cols != 40 {
		t.Fatalf("hello %s err=%v", evs[0].Data, err)
	}
	sawSecond, sawExit := false, false
	for _, e := range evs[1:] {
		if e.Name == "frame" && strings.Contains(e.Data, "SECOND") {
			sawSecond = true
		}
		if e.Name == "exited" && strings.Contains(e.Data, `"code":7`) {
			sawExit = true
		}
	}
	if !sawSecond || !sawExit {
		t.Fatalf("second=%v exit=%v events=%+v", sawSecond, sawExit, evs)
	}
}

func TestSessionScreenRefusesUnknownID(t *testing.T) {
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	resp, err := http.Get(ts.URL + "/api/session-screen?id=nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
}

func TestFeedSkipsASlowStreamAndSendsItAFullFrameNext(t *testing.T) {
	t.Parallel()
	f := newScreenFeeds()
	fast, cancelFast := f.subscribe("x")
	slow, cancelSlow := f.subscribe("x")
	defer cancelFast()
	defer cancelSlow()
	a := domain.ScreenRuns{Cols: 5, Rows: 1, Lines: []domain.ScreenRunRow{runs("a")}}
	b := a
	b.Lines = []domain.ScreenRunRow{runs("b")}
	c := a
	c.Lines = []domain.ScreenRunRow{runs("c")}
	f.publish("x", a) // both take it
	<-fast
	f.publish("x", b) // fast takes it; slow still holds a → its buffer is full → skipped
	<-fast
	first := <-slow // the slow reader finally drains a…
	if first.Screen.Lines[0].Runs[0].Text != "a" || first.Full {
		t.Fatalf("slow first = %+v", first)
	}
	f.publish("x", c) // …and the next screen it gets is marked full, so b's miss never shows
	<-fast
	next := <-slow
	if next.Screen.Lines[0].Runs[0].Text != "c" || !next.Full {
		t.Fatalf("slow after skip = %+v, want c marked full", next)
	}
}

func TestSessionScreenGoneAfterRemove(t *testing.T) {
	s := testSession(t, `exit 0`)
	<-s.Done()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = domain.Sessions().Remove(s.Info().ID)
	}()
	evs := readConsoleSSE(t, ts, string(s.Info().ID), 20, 5*time.Second) // hello, exited, stray frames, gone
	if len(evs) == 0 || evs[len(evs)-1].Name != "gone" {
		t.Fatalf("events %+v", evs)
	}
}

// An already-exited session reports its exit exactly once on attach, even
// when a change signal is still pending from its last output.
func TestSessionScreenExitedSessionReportsExitOnce(t *testing.T) {
	s := testSession(t, `printf 'bye'; exit 3`)
	<-s.Done()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	evs := readConsoleSSE(t, ts, string(s.Info().ID), 20, 1500*time.Millisecond)
	n := 0
	for _, e := range evs {
		if e.Name == "exited" {
			n++
		}
	}
	if n != 1 || evs[0].Name != "hello" || evs[1].Name != "exited" {
		t.Fatalf("exited ×%d, events %+v", n, evs)
	}
}
