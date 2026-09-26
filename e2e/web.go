package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/web"
)

// startWeb serves the sandbox's local repo with a real in-process gg web
// ([input] web = true) and waits until its steering presence is live, so a
// scenario's `gg session …` runs reach it exactly as they reach a user's page.
// The server is stopped, and awaited, when the scenario ends.
func startWeb(t *testing.T, sb *Sandbox) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	exited := make(chan struct{})
	var serveErr error
	go func() {
		defer close(exited)
		serveErr = web.Serve(ctx, sb.LocalDir, "127.0.0.1:0", false, nil)
	}()
	t.Cleanup(func() {
		cancel()
		<-exited
	})
	svc := domain.Open(sb.LocalDir)
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dir := config.SessionSteerDir(cd, top)
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		if _, ok := steer.Live(dir, steer.WebPresence); ok {
			return
		}
		select {
		case <-exited:
			t.Fatalf("gg web exited before claiming its presence: %v", serveErr)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("gg web never claimed its steering presence")
}
