package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/steer"
)

// TestSessionNavigateWithALinkReachesAnotherCheckoutsInbox is ruling P3/P7's
// SERIAL cross-checkout headline test: a link naming a checkout OTHER than
// the caller's cwd must steer THAT checkout's own real inbox, computed via
// config.SessionSteerDir, not the inbox dir runSession was given as its
// (test-only) fallback.
//
// SERIAL — no t.Parallel(): sets XDG_STATE_HOME (t.Setenv) and the
// package-global RepoStatePath (saved/restored). Safe only because this file
// carries no t.Parallel() test, so the Go test runner never interleaves it
// with a parallel one — the serial phase of the package finishes first.
func TestSessionNavigateWithALinkReachesAnotherCheckoutsInbox(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	statePath := filepath.Join(t.TempDir(), "repos.toml")
	prevState := RepoStatePath
	RepoStatePath = statePath
	defer func() { RepoStatePath = prevState }()

	// B: the checkout the link will name. Registered in the registry (as a
	// second, unrelated repo would be after `gg` has opened it once), NOT as
	// the current directory.
	repoB := newCLIRepo(t)
	if err := repos.Touch(statePath, repoB, "workmate", time.Now()); err != nil {
		t.Fatalf("repos.Touch: %v", err)
	}

	ctx := context.Background()
	svcB := domain.Open(repoB)
	commonDirB, err := svcB.GitCommonDir(ctx)
	if err != nil {
		t.Fatalf("GitCommonDir(B): %v", err)
	}
	inboxB := config.SessionSteerDir(commonDirB, repoB)
	if inboxB == "" {
		t.Fatal("SessionSteerDir(B) = \"\"")
	}
	livePresence(t, inboxB)

	// Build the link FROM B (as `gg link` run inside B would print it).
	var lout bytes.Buffer
	if code := runLink(statePath, svcB, repoB, []string{"README.md:2"}, &lout, os.Stderr); code != 0 {
		t.Fatalf("gg link: exit %d", code)
	}
	link := strings.TrimSpace(lout.String())

	// Run `gg session navigate <link>` from a cwd that is NOT B: no repo at
	// all, like an agent working out of /tmp.
	cwd := t.TempDir()
	svcCwd := domain.Open(cwd)
	fallback := t.TempDir() // the injected inbox — must NOT receive the command
	var out, errb bytes.Buffer
	if code := runSession(fallback, svcCwd, []string{"navigate", link, "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}

	got := steer.Drain(inboxB)
	if len(got) != 1 {
		t.Fatalf("B's inbox = %+v, want one navigate command", got)
	}
	if got[0].Cmd != "navigate" || got[0].File != "README.md" {
		t.Errorf("posted %+v, want a navigate on README.md", got[0])
	}
	if leftover := steer.Drain(fallback); len(leftover) != 0 {
		t.Errorf("fallback inbox got %+v, want nothing — the command must go to B, not the cwd's own inbox", leftover)
	}
}
