package git

import (
	"context"
	"testing"
)

func TestRemoteURLReadsTheConfiguredURL(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	if _, err := r.Runner.Run(ctx, "git remote add", []string{"remote", "add", "origin", "git@github.com:homeend/gigagit.git"}); err != nil {
		t.Fatal(err)
	}
	got, err := r.RemoteURL(ctx, "origin")
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	if got != "git@github.com:homeend/gigagit.git" {
		t.Errorf("RemoteURL = %q", got)
	}
	if _, err := r.RemoteURL(ctx, "nope"); err == nil {
		t.Error("RemoteURL of an unknown remote should fail")
	}
}
