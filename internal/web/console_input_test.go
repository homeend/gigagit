package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func waitSessionText(t *testing.T, s *domain.AgentSession, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.Text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, s.Text())
}

func TestSessionInputTypesKeysAndPaste(t *testing.T) {
	s := testSession(t, `read x; echo "GOT:$x"; read y; echo "PASTED:$y"; sleep 3`)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var out map[string]any
	body := `{"id":"` + string(s.Info().ID) + `","keys":[{"k":"char","text":"hi"},{"k":"char","mod":1,"text":"!"},{"k":"enter"}]}`
	if code := postJSON(t, ts, "/api/session-input", body, "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("code %d %v", code, out)
	}
	waitSessionText(t, s, "GOT:hi!")
	body = `{"id":"` + string(s.Info().ID) + `","paste":"pasted text\n"}`
	if code := postJSON(t, ts, "/api/session-input", body, "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("code %d %v", code, out)
	}
	waitSessionText(t, s, "PASTED:pasted text")
}

func TestSessionInputRefusals(t *testing.T) {
	s := testSession(t, `exit 0`)
	<-s.Done()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var out map[string]any
	id := string(s.Info().ID)
	for _, c := range []struct {
		body, ctype, origin string
		want                int
	}{
		{`{"id":"` + id + `","keys":[{"k":"enter"}]}`, "application/json", "", http.StatusConflict}, // exited
		{`{"id":"nope","keys":[{"k":"enter"}]}`, "application/json", "", http.StatusNotFound},
		{`{"id":"` + id + `","keys":[{"k":"bogus"}]}`, "application/json", "", http.StatusBadRequest},
		{`{"id":"` + id + `"}`, "text/plain", "", http.StatusUnsupportedMediaType},
		{`{"id":"` + id + `"}`, "application/json", "http://evil.example", http.StatusForbidden},
	} {
		if code := postJSON(t, ts, "/api/session-input", c.body, c.ctype, c.origin, &out); code != c.want {
			t.Fatalf("%s → %d, want %d", c.body, code, c.want)
		}
	}
}

func TestSessionSizeClampsAndResizes(t *testing.T) {
	s := testSession(t, `sleep 5`)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var out struct{ Cols, Rows int }
	body := `{"id":"` + string(s.Info().ID) + `","cols":9999,"rows":2}`
	if code := postJSON(t, ts, "/api/session-size", body, "application/json", "", &out); code != http.StatusOK || out.Cols != 500 || out.Rows != 5 {
		t.Fatalf("code %d out %+v", code, out)
	}
	if sc := s.Screen(); sc.Cols != 500 || sc.Rows != 5 {
		t.Fatalf("emulator %dx%d", sc.Cols, sc.Rows)
	}
	if code := postJSON(t, ts, "/api/session-size", `{"id":"nope","cols":80,"rows":24}`, "application/json", "", &out); code != http.StatusNotFound {
		t.Fatalf("unknown id → %d", code)
	}
}
