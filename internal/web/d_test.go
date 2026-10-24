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
