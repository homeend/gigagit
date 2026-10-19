package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Review focus 3: the key rule, with the spec's Linux test vector
// (2026-10-15-gg-notes-store-format §3.2).
func TestNotesKeyVector(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		if got := repoKey("/work/gigagit/.git"); got != "d10200a3a219735f" {
			t.Fatalf("repoKey(/work/gigagit/.git) = %q, want d10200a3a219735f", got)
		}
	}
	dir := filepath.Join(t.TempDir(), "repo", ".git")
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	if got, want := repoKey(dir), hex.EncodeToString(sum[:8]); got != want {
		t.Fatalf("repoKey = %q, want %q", got, want)
	}
}

// A real repo resolves to <XDG_STATE_HOME>/gg/notes/<key> with the key of
// its common dir — the path and key gg overview store prints.
func TestNotesStorePathResolved(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // t.Setenv forbids t.Parallel
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info := svc.NotesStorePath(ctx)
	wantKey := repoKey(cd)
	if info.Key != wantKey || len(info.Key) != 16 {
		t.Fatalf("Key = %q, want %q", info.Key, wantKey)
	}
	wantDir := filepath.Join(stateBaseDir("notes"), wantKey)
	if info.Dir != wantDir {
		t.Fatalf("Dir = %q, want %q (repo %s)", info.Dir, wantDir, dir)
	}
	if !strings.HasPrefix(info.Dir, filepath.Join(os.Getenv("XDG_STATE_HOME"), "gg", "notes")) {
		t.Fatalf("Dir %q is not under the test's XDG_STATE_HOME", info.Dir)
	}
}

// Review focus 4: an injected store reports ITS directory and no key.
func TestNotesStorePathInjected(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	want := t.TempDir()
	svc.UseNotesDir(want)
	info := svc.NotesStorePath(context.Background())
	if info.Dir != want || info.Key != "" {
		t.Fatalf("NotesStorePath = %+v, want Dir %q and an empty key", info, want)
	}
}

// Notes disabled: nothing to report.
func TestNotesStorePathDisabled(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	svc.disableNotesForTest()
	if info := svc.NotesStorePath(context.Background()); info != (NotesStoreInfo{}) {
		t.Fatalf("NotesStorePath = %+v, want zero", info)
	}
}
