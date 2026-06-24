package git

import (
	"context"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestLogScopedDateOrderFlag(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	repo := &Repo{Runner: f}

	repo.LogScoped(context.Background(), 10, 0, LogScope{}, true)
	if !slices.Contains(f.Calls[len(f.Calls)-1].Argv, "--date-order") {
		t.Error("dateOrder=true must include --date-order")
	}
	f.Calls = nil
	repo.LogScoped(context.Background(), 10, 0, LogScope{}, false)
	if slices.Contains(f.Calls[len(f.Calls)-1].Argv, "--date-order") {
		t.Error("dateOrder=false must omit --date-order")
	}
}
