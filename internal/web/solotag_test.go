package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// A tag is a ref to git log like any other — soloing one narrows the feed to
// its history (the TUI's tagSoloRow). The scope-must-render invariant holds:
// the tag resolves against a fresh read before the scope is entered.
func TestSoloTag(t *testing.T) {
	dir := soloRepo(t)
	// Tag the main-only state; feature's extra commit is outside its history.
	gitRun(t, dir, "tag", "v-main", "main")
	ts := serve(t, New(domain.Open(dir)))

	if code := setSoloHTTP(t, ts, `{"branch":"v-main"}`); code != http.StatusOK {
		t.Fatalf("solo tag code = %d", code)
	}
	subs, solo := commitSubjects(t, ts)
	if solo != "v-main" {
		t.Errorf("solo = %q", solo)
	}
	for _, s := range subs {
		if s == "only on feature" {
			t.Errorf("feature-only commit leaked into the tag scope: %v", subs)
		}
	}
	found := false
	for _, s := range subs {
		if s == "on main" {
			found = true
		}
	}
	if !found {
		t.Errorf("tag history missing its own commit: %v", subs)
	}

	// Clearing still works.
	if code := setSoloHTTP(t, ts, `{"branch":""}`); code != http.StatusOK {
		t.Fatalf("clear code = %d", code)
	}
	if _, solo := commitSubjects(t, ts); solo != "" {
		t.Errorf("solo after clear = %q", solo)
	}
}

func TestSoloUnknownRefStill404s(t *testing.T) {
	dir := soloRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	if code := setSoloHTTP(t, ts, `{"branch":"ghost"}`); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404 (neither branch nor tag)", code)
	}
}
