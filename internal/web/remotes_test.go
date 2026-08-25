package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestRemotesEndpoint(t *testing.T) {
	t.Parallel()
	_, clone := cloneWithOrigin(t)
	ts := serve(t, New(domain.Open(clone)))

	var got struct {
		Remotes []struct {
			Name   string `json:"name"`
			Remote string `json:"remote"`
			Branch string `json:"branch"`
			Hash   string `json:"hash"`
		} `json:"remotes"`
		Truncated bool `json:"truncated"`
	}
	if code := getJSON(t, ts, "/api/remotes", &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	var main *struct {
		Name   string `json:"name"`
		Remote string `json:"remote"`
		Branch string `json:"branch"`
		Hash   string `json:"hash"`
	}
	for i := range got.Remotes {
		if got.Remotes[i].Name == "origin/main" {
			main = &got.Remotes[i]
		}
	}
	if main == nil {
		t.Fatalf("no origin/main in %+v", got.Remotes)
	}
	if main.Remote != "origin" || main.Branch != "main" || main.Hash == "" {
		t.Errorf("origin/main = %+v", *main)
	}
	if got.Truncated {
		t.Error("truncated on a one-remote fixture")
	}
}
