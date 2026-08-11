package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/observ"
)

type sessionErrorsWire struct {
	Errors []struct {
		Time   string `json:"time"`
		Source string `json:"source"`
		Detail string `json:"detail"`
	} `json:"errors"`
	Truncated bool   `json:"truncated"`
	LogPath   string `json:"log_path"`
}

// TestSessionErrors: a genuinely failed operation (duplicate branch name)
// lands in the failure ring and comes back newest-first from the endpoint.
// The ring is process-global, so the test resets it first (web tests run
// sequentially — no t.Parallel in this package).
func TestSessionErrors(t *testing.T) {
	observ.ResetFailures()
	t.Cleanup(observ.ResetFailures)
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "branch", "taken")
	ts := serve(t, New(domain.Open(dir)))

	var got sessionErrorsWire
	if code := getJSON(t, ts, "/api/session-errors", &got); code != http.StatusOK {
		t.Fatalf("GET code = %d", code)
	}
	if len(got.Errors) != 0 {
		t.Fatalf("fresh ring not empty: %+v", got.Errors)
	}
	if got.LogPath == "" {
		t.Errorf("log_path empty")
	}

	// a failed create-branch is a genuine failure → ring entry
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"create-branch","name":"taken"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] == true {
		t.Fatal("duplicate create should fail")
	}
	if code := getJSON(t, ts, "/api/session-errors", &got); code != http.StatusOK {
		t.Fatal("second GET failed")
	}
	found := false
	for _, e := range got.Errors {
		if strings.Contains(e.Detail, "taken") && e.Source != "" && e.Time != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed create not in ring: %+v", got.Errors)
	}
}

// TestSessionErrorsCap: the endpoint caps its payload and reports the cut.
func TestSessionErrorsCap(t *testing.T) {
	observ.ResetFailures()
	t.Cleanup(observ.ResetFailures)
	for i := 0; i < 150; i++ {
		observ.NoteFailure("test", fmt.Errorf("boom %03d", i))
	}
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	var got sessionErrorsWire
	if code := getJSON(t, ts, "/api/session-errors", &got); code != http.StatusOK {
		t.Fatalf("GET code = %d", code)
	}
	if len(got.Errors) != 100 || !got.Truncated {
		t.Fatalf("len = %d truncated = %v, want 100/true", len(got.Errors), got.Truncated)
	}
	// newest first: the last noted failure leads
	if !strings.Contains(got.Errors[0].Detail, "149") {
		t.Errorf("first row = %+v, want the newest (149)", got.Errors[0])
	}
}
