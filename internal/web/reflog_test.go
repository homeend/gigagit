package web

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestReflogEndpoint(t *testing.T) {
	t.Parallel()
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

// The sidebar pages the reflog by asking for a bigger window, so limit has to
// mean "this many rows" AND truncated has to mean "there is a next page" —
// otherwise the "show more" row cannot know whether to keep offering itself.
func TestReflogEndpointPages(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 8)
	ts := serve(t, New(domain.Open(dir)))

	page := func(q string) (int, bool, string) {
		t.Helper()
		var got struct {
			Entries []struct {
				Selector string `json:"selector"`
			} `json:"entries"`
			Truncated bool `json:"truncated"`
		}
		if code := getJSON(t, ts, "/api/reflog"+q, &got); code != http.StatusOK {
			t.Fatalf("%s: code = %d", q, code)
		}
		last := ""
		if n := len(got.Entries); n > 0 {
			last = got.Entries[n-1].Selector
		}
		return len(got.Entries), got.Truncated, last
	}

	if n, trunc, last := page("?limit=3"); n != 3 || !trunc || last != "HEAD@{2}" {
		t.Errorf("limit=3 → %d entries, truncated %v, last %q; want 3, true, HEAD@{2}", n, trunc, last)
	}
	// The next page is the same read with a bigger window: every earlier row
	// again, plus the new ones, and no more pages left.
	if n, trunc, last := page("?limit=6"); n != 6 || !trunc || last != "HEAD@{5}" {
		t.Errorf("limit=6 → %d entries, truncated %v, last %q; want 6, true, HEAD@{5}", n, trunc, last)
	}
	if n, trunc, last := page("?limit=100"); n != 8 || trunc || last != "HEAD@{7}" {
		t.Errorf("limit=100 → %d entries, truncated %v, last %q; want 8, false, HEAD@{7}", n, trunc, last)
	}
	// A junk or absent limit keeps the historical window rather than erroring.
	if n, trunc, _ := page("?limit=nope"); n != 8 || trunc {
		t.Errorf("limit=nope → %d entries, truncated %v; want the default window", n, trunc)
	}
}
