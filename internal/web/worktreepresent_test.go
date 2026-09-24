package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestWorktreePresentEndpoint(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var got struct{ Present bool }
	if code := getJSON(t, ts, "/api/worktree-present?path=f.txt", &got); code != http.StatusOK || !got.Present {
		t.Fatalf("f.txt: code=%d present=%v, want 200 true", code, got.Present)
	}
	got.Present = true
	if code := getJSON(t, ts, "/api/worktree-present?path=gone.txt", &got); code != http.StatusOK || got.Present {
		t.Fatalf("gone.txt: code=%d present=%v, want 200 false", code, got.Present)
	}
	got.Present = true
	if code := getJSON(t, ts, "/api/worktree-present?path=../../etc/passwd", &got); code != http.StatusOK || got.Present {
		t.Fatalf("escaping path: code=%d present=%v, want 200 false", code, got.Present)
	}
	if code := getJSON(t, ts, "/api/worktree-present", nil); code != http.StatusBadRequest {
		t.Fatalf("no path: code=%d, want 400", code)
	}
}
