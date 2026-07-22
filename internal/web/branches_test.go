package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestBranchesEndpoint(t *testing.T) {
	dir := twoBranchRepo(t) // on main, plus "side"
	ts := serve(t, New(domain.Open(dir)))
	var body struct {
		Branches []struct {
			Name   string `json:"name"`
			IsHead bool   `json:"is_head"`
			Hash   string `json:"hash"`
			Time   int64  `json:"time"`
		} `json:"branches"`
	}
	if code := getJSON(t, ts, "/api/branches", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Branches) != 2 {
		t.Fatalf("branches = %+v", body.Branches)
	}
	heads := 0
	for _, b := range body.Branches {
		if b.Name != "main" && b.Name != "side" {
			t.Errorf("unexpected branch %q", b.Name)
		}
		if b.Hash == "" || b.Time == 0 {
			t.Errorf("missing hash/time: %+v", b)
		}
		if b.IsHead {
			heads++
			if b.Name != "main" {
				t.Errorf("is_head on %q, want main", b.Name)
			}
		}
	}
	if heads != 1 {
		t.Errorf("heads = %d, want 1", heads)
	}
}
