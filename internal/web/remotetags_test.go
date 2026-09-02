package web

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// The sidebar's tag rows carry the TUI's ▲: whether the default remote has
// the tag, per the last listing. Before any listing the rows say nothing
// (no wrong "local only"); after one, each row carries a verdict.
func TestTagsCarryRemoteVerdictAfterAListing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "-q")
	gitRun(t, dir, "remote", "add", "origin", remote)
	gitRun(t, dir, "tag", "pushed")
	gitRun(t, dir, "tag", "local-only")
	gitRun(t, dir, "push", "-q", "origin", "pushed")

	svc := domain.Open(dir)
	s := New(svc)
	ts := serve(t, s)

	type row struct {
		Name   string          `json:"name"`
		Remote json.RawMessage `json:"remote"`
	}
	var got struct {
		Tags        []row `json:"tags"`
		RemoteKnown bool  `json:"remote_known"`
	}
	if code := getJSON(t, ts, "/api/tags", &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if got.RemoteKnown {
		t.Error("remote_known = true before any listing")
	}
	for _, r := range got.Tags {
		if r.Remote != nil {
			t.Errorf("%s carries remote=%s before any listing", r.Name, r.Remote)
		}
	}

	s.refreshRemoteTags(context.Background(), svc)

	got.Tags, got.RemoteKnown = nil, false
	if code := getJSON(t, ts, "/api/tags", &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if !got.RemoteKnown {
		t.Fatal("remote_known = false after a listing")
	}
	want := map[string]string{"pushed": "true", "local-only": "false"}
	for _, r := range got.Tags {
		if w, ok := want[r.Name]; ok && string(r.Remote) != w {
			t.Errorf("%s remote = %s, want %s", r.Name, r.Remote, w)
		}
	}
}

// A re-root must not carry the previous repo's listing over: the verdict is
// keyed to the service it was taken for.
func TestRemoteTagListingIsPerService(t *testing.T) {
	t.Parallel()
	a, b := domain.Open(newRepoDir(t, 1)), domain.Open(newRepoDir(t, 1))
	s := New(a)
	s.storeRemoteTags(a, map[string]bool{"v1": true})
	if _, known := s.remoteTagNames(a); !known {
		t.Fatal("listing for a not known")
	}
	if _, known := s.remoteTagNames(b); known {
		t.Error("b inherits a's listing")
	}
	// A successful push-tag folds into a known listing; a delete drops it.
	s.markRemoteTag(a, "v2", true)
	s.markRemoteTag(a, "v1", false)
	names, _ := s.remoteTagNames(a)
	if !names["v2"] || names["v1"] {
		t.Errorf("after mark: %v, want v2 only", names)
	}
}
