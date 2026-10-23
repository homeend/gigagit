package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/filelock"
)

// Probe round 2: the quarantine and the lock under a read-only state dir.

// two corrupt files quarantined in the same second share one name
// (<path>.corrupt-<unix>): the second rename silently replaces the first, so
// the hand edit the first quarantine "kept" is lost.
func TestCorruptQuarantineSameSecondKeepsBoth(t *testing.T) {
	p := tmpFile(t)
	os.MkdirAll(filepath.Dir(p), 0o755)
	for _, body := range []string{"first = [corrupt", "second = [corrupt"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Update(p, func(ws []Entry) []Entry { return ws }); err != nil {
			t.Fatal(err)
		}
	}
	matches, _ := filepath.Glob(p + ".corrupt-*")
	var bodies []string
	for _, m := range matches {
		b, _ := os.ReadFile(m)
		bodies = append(bodies, string(b))
	}
	joined := strings.Join(bodies, "|")
	if !strings.Contains(joined, "first") || !strings.Contains(joined, "second") {
		t.Fatalf("quarantined files %v hold %q: a same-second quarantine overwrote the earlier one", matches, joined)
	}
}

// a read-only state dir. The lock create fails with
// ErrPermission, which filelock retries (Windows pending-delete) for its whole
// Wait budget, then reports "lock is held" — a 2 s stall on the TUI's Update
// thread (the popup saves synchronously) and the wrong reason.
func TestReadOnlyDirFailsFastWithTheRealReason(t *testing.T) {
	p := tmpFile(t)
	dir := filepath.Dir(p)
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(p, []byte("format = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	start := time.Now()
	_, err := Update(p, func(ws []Entry) []Entry { return append(ws, Entry{Name: "x"}) })
	took := time.Since(start)
	if err == nil {
		t.Skip("running as root: chmod does not bite")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory mode")
	}
	if took > 500*time.Millisecond || errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("Update on a read-only dir took %v and said %v (want a fast permission error)", took, err)
	}
}
