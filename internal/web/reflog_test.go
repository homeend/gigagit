package web

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestReflogEndpoint(t *testing.T) {
	dir := newRepoDir(t, 3)
	ts := serve(t, New(domain.Open(dir)))

	var got struct {
		Entries []struct {
			Selector string `json:"selector"`
			Hash     string `json:"hash"`
			Short    string `json:"short"`
			Subject  string `json:"subject"`
			Rel      string `json:"rel"`
		} `json:"entries"`
		Truncated bool `json:"truncated"`
	}
	if code := getJSON(t, ts, "/api/reflog", &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries = %d, want 3 (one per commit)", len(got.Entries))
	}
	e := got.Entries[0]
	if e.Selector != "HEAD@{0}" {
		t.Errorf("selector = %q", e.Selector)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(e.Hash) {
		t.Errorf("hash = %q, want a full sha", e.Hash)
	}
	if e.Short == "" || e.Subject == "" {
		t.Errorf("entry = %+v, want short + subject", e)
	}
	if got.Truncated {
		t.Error("truncated on a 3-entry reflog")
	}
}
