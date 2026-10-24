package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The walks tell agents from terminals (the TUI's alt+a / alt+t rings).
func TestSessionsWireMarksTerminals(t *testing.T) {
	t.Parallel()
	w := sessionsWire([]domain.SessionInfo{{ID: "a", Label: "claude"}, {ID: "b", Label: "Terminal", Terminal: true}}, nil)
	if w[0].Terminal || !w[1].Terminal {
		t.Fatalf("%+v", w)
	}
}

// Scroll mode reads a frozen history: the scrollback, then the screen, as
// styled runs.
func TestSessionHistoryEndpoint(t *testing.T) {
	s := testSession(t, `i=0; while [ $i -lt 15 ]; do echo "l$i"; i=$((i+1)); done; printf 'END'; sleep 5`)
	waitSessionText(t, s, "END")
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var h struct {
		Lines     [][]domain.ScreenRun `json:"lines"`
		Cols      int                  `json:"cols"`
		ScreenTop int                  `json:"screen_top"`
	}
	if code := getJSON(t, ts, "/api/session-history?id="+string(s.Info().ID), &h); code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if len(h.Lines) != 16 || h.Cols != 40 || h.ScreenTop != 6 {
		t.Fatalf("len=%d cols=%d top=%d", len(h.Lines), h.Cols, h.ScreenTop)
	}
	if h.Lines[0][0].Text != "l0" || h.Lines[15][0].Text != "END" {
		t.Fatalf("first %+v last %+v", h.Lines[0], h.Lines[15])
	}
	if code := getJSON(t, ts, "/api/session-history?id=nope", nil); code != http.StatusNotFound {
		t.Errorf("unknown id = %d", code)
	}
}

func postMouse(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url+"/api/session-mouse", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// The mouse reaches a program that asked for it; a wheel over an alternate
// screen without mouse tracking becomes ↑/↓; otherwise nothing is sent.
func TestSessionMouseEndpoint(t *testing.T) {
	s := testSession(t, `stty -echo -icanon; printf '\033[?1000hMOUSE'; cat -v`)
	waitSessionText(t, s, "MOUSE")
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	id := string(s.Info().ID)
	code, out := postMouse(t, ts.URL, fmt.Sprintf(`{"id":%q,"kind":"press","button":"left","x":2,"y":1}`, id))
	if code != http.StatusOK || out["sent"] != "mouse" {
		t.Fatalf("press = %d %v", code, out)
	}
	waitSessionText(t, s, "^[[M")
	for _, bad := range []string{`{"id":"` + id + `","kind":"poke","button":"left"}`, `{"id":"` + id + `","kind":"press","button":"thumb"}`, `{"id":"` + id + `","kind":"press","button":"left","x":-1}`} {
		if code, _ := postMouse(t, ts.URL, bad); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
	if code, _ := postMouse(t, ts.URL, `{"id":"nope","kind":"wheel","button":"wheelup"}`); code != http.StatusNotFound {
		t.Errorf("unknown id = %d", code)
	}

	alt := testSession(t, `stty -echo -icanon; printf '\033[?1049hALT'; cat -v`)
	waitSessionText(t, alt, "ALT")
	code, out = postMouse(t, ts.URL, fmt.Sprintf(`{"id":%q,"kind":"wheel","button":"wheelup","x":0,"y":0}`, string(alt.Info().ID)))
	if code != http.StatusOK || out["sent"] != "keys" {
		t.Fatalf("alt wheel = %d %v", code, out)
	}
	waitSessionText(t, alt, "^[")

	plain := testSession(t, `printf 'PLAIN'; sleep 5`)
	waitSessionText(t, plain, "PLAIN")
	if _, out := postMouse(t, ts.URL, fmt.Sprintf(`{"id":%q,"kind":"wheel","button":"wheeldown"}`, string(plain.Info().ID))); out["sent"] != "none" {
		t.Errorf("plain wheel = %v, want none (the page scrolls back instead)", out)
	}
}

// Frames say whether the program takes the mouse.
func TestSessionScreenCarriesMouseMode(t *testing.T) {
	s := testSession(t, `printf '\033[?1000hMOUSE'; sleep 5`)
	waitSessionText(t, s, "MOUSE")
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	evs := readConsoleSSE(t, ts, string(s.Info().ID), 1, 5*time.Second)
	if len(evs) == 0 || !strings.Contains(evs[0].Data, `"mouse":true`) {
		t.Fatalf("hello = %+v", evs)
	}
}

// The TUI's alt+a / alt+t / alt+A / alt+T rings and alt+q / alt+Q screens
// (console.go cycleSessionsIn, worktree_walk.go walkWorktree).
func TestWalksJS(t *testing.T) {
	t.Parallel()
	var mod strings.Builder
	for _, fn := range []string{"wtBase", "walkRing", "walkNext", "walkLine", "screenStops", "walkScreens"} {
		mod.WriteString(jsFunc(t, "walk.js", fn) + "\n")
	}
	src := mod.String() + `
const order = ["/r", "/r/wt-a", "/r/wt-b"];
const S = (id, wt, started, extra) => ({ id, label: id.toUpperCase(), worktree: wt, state: "running", started, ...extra });
const ss = [
  S("b1", "/r/wt-b", "2026-10-24T10:00:00Z"), S("a2", "/r/wt-a", "2026-10-24T10:05:00Z"), S("a1", "/r/wt-a", "2026-10-24T10:01:00Z"),
  S("t1", "/r", "2026-10-24T09:00:00Z", { terminal: true }), S("x", "/r/wt-a", "2026-10-24T09:00:00Z", { state: "exited" }), S("o", "/other", "2026-10-24T09:00:00Z"),
];
const idx = (p) => order.indexOf(p);
const ring = walkRing(ss, order, false, "");
const out = [
  ring.map((s) => s.id),
  walkRing(ss, order, true, "").map((s) => s.id),
  walkRing(ss, order, false, "/r/wt-b").map((s) => s.id),
  walkNext(ring, "", false, 0, idx, false, false),            // gg's keyboard on the main worktree: the first at or below
  walkNext(ring, "", false, 2, idx, false, false),            // viewing wt-b: its own first
  walkNext(ring, "a2", true, 0, idx, false, false),           // bound: the next
  walkNext(ring, "b1", true, 0, idx, false, false),           // around again
  walkNext([ring[0]], "a1", true, 0, idx, false, false),      // the only one, bound
  walkNext([ring[0]], "a1", true, 0, idx, false, true),
  walkNext([ring[0]], "a1", true, 0, idx, true, false),
  walkNext([], "", false, 0, idx, false, false),
  walkNext([], "", false, 0, idx, true, false),
  walkNext([], "", false, 0, idx, false, true),               // scoped, none: nothing
  walkLine(ring[1], 1, 3, false), walkLine(ss[3], 0, 1, true),
];
const stops = screenStops(ss, order, "/r/wt-a");
out.push(stops.map((s) => (s ? s.id : "page")));
out.push(walkScreens(stops, "", false, "/r/wt-a"), walkScreens(stops, "a2", false, "/r/wt-a"), walkScreens(stops, "", true, "/r/wt-a"), walkScreens(screenStops(ss, order, "/r/zz"), "", false, "/r/zz"));
console.log(JSON.stringify(out));
`
	want := `[["a1","a2","b1"],["t1"],["b1"],{"i":0},{"i":2},{"i":2},{"i":0},` +
		`{"msg":"the only running agent session in this repository — already focused"},` +
		`{"msg":"the only running agent session in this worktree — already bound"},` +
		`{"msg":"the only running terminal in this repository — already focused"},` +
		`{"msg":"no running agent session in this repository — start one from the . menu of a worktree or a checked-out branch"},` +
		`{"msg":"no running terminal in this repository — open one from the . menu of a worktree or a checked-out branch"},` +
		`{},` +
		`"A2 in wt-a — agent 2 of 3","T1 in r — terminal 1 of 1",` +
		`["page","a1","a2"],` +
		`{"i":1,"msg":"A1 in wt-a — 2 of 3"},{"i":0,"msg":"wt-a — your windows, 1 of 3"},{"i":2,"msg":"A2 in wt-a — 3 of 3"},{"msg":"no running agent or terminal in this worktree"}]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("walks =\n%s\nwant\n%s", got, want)
	}
}

func TestWalksWired(t *testing.T) {
	t.Parallel()
	c := readStaticSrc(t, "console.js")
	for _, want := range []string{
		"if (walkKey(e)) return true; // gg's alt keys, even inside a bound console",
		`if (e.altKey && e.code === "KeyF" && !e.ctrlKey && !e.metaKey)`,
		`if (e.altKey && e.code === "KeyB" && !e.ctrlKey && !e.metaKey)`,
		"alt+f maximize",
		"alt+b",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("console.js lacks %s", want)
		}
	}
	if !strings.Contains(readStaticSrc(t, "keys.js"), "walkKey(e)") {
		t.Error("keys.js: the walk keys work with no console shown")
	}
}

