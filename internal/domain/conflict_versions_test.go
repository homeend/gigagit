package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConflictFileVersionsBothSides(t *testing.T) {
	dir := mergeConflictDir(t) // paused merge of feature into main, one conflicted file
	svc := svcAt(dir)
	// Discover the conflicted path from status (fixture-agnostic).
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conf := st.Conflicts()
	if len(conf) == 0 {
		t.Fatal("fixture has no conflicts")
	}
	path := conf[0].Path

	local, base, remote, cleanup, err := svc.ConflictFileVersions(context.Background(), path, conf[0].ConflictHasBase())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	lb, _ := os.ReadFile(local)
	rb, _ := os.ReadFile(remote)
	if len(lb) == 0 || len(rb) == 0 {
		t.Fatalf("local/remote must carry each side's content (%d/%d bytes)", len(lb), len(rb))
	}
	if string(lb) == string(rb) {
		t.Error("local and remote sides must differ in a conflict")
	}
	// divergedDir (via mergeConflictDir) deterministically gives the
	// conflicted file "ours\n" on main (stage :2 = local, the merge's HEAD
	// side), "theirs\n" on feature (stage :3 = remote), and "base\n" at the
	// merge base (stage :1). Assert exact side identity, not just
	// non-empty/differ — a :2/:3 swap in ConflictFileVersions would corrupt
	// every external-mergetool run while still passing a non-empty/differ
	// check.
	if got := string(lb); got != "ours\n" {
		t.Errorf("local (stage :2) = %q, want %q", got, "ours\n")
	}
	if got := string(rb); got != "theirs\n" {
		t.Errorf("remote (stage :3) = %q, want %q", got, "theirs\n")
	}
	if conf[0].ConflictHasBase() {
		if _, err := os.Stat(base); err != nil {
			t.Errorf("base temp missing: %v", err)
		}
		bb, err := os.ReadFile(base)
		if err != nil {
			t.Fatalf("read base temp: %v", err)
		}
		if got := string(bb); got != "base\n" {
			t.Errorf("base (stage :1) = %q, want %q", got, "base\n")
		}
	}
	// Temp names keep the real extension for tool syntax highlighting.
	if ext := filepath.Ext(path); ext != "" {
		for _, p := range []string{local, base, remote} {
			if !strings.HasSuffix(p, ext) {
				t.Errorf("%s should keep extension %s", p, ext)
			}
		}
	}
	cleanup()
	for _, p := range []string{local, base, remote} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("cleanup left %s", p)
		}
	}
}

func TestConflictFileVersionsNoBase(t *testing.T) {
	dir := mergeConflictDir(t)
	svc := svcAt(dir)
	st, _ := svc.Status(context.Background())
	path := st.Conflicts()[0].Path
	_, base, _, cleanup, err := svc.ConflictFileVersions(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatalf("hasBase=false must still create an empty base temp: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("base must be empty when hasBase=false, got %d bytes", len(data))
	}
}
