package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/gittest"
)

// previewRepo is a real repo: main has c1; feat/x branches off c1 and adds
// two commits touching a.txt and b.txt; main then adds c2 touching m.txt
// (so the tips have diverged and the three-dot diff is exactly {a.txt, b.txt}).
func previewRepo(t *testing.T) (string, *Service) {
	t.Helper()
	dir, svc := newRealRepo(t)
	svc.UsePreviewsDir(t.TempDir())
	gittest.Run(t, dir, "checkout", "-q", "-b", "feat/x")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add a")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gittest.Run(t, dir, "add", "b.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add b")
	gittest.Run(t, dir, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("m\n"), 0o644)
	gittest.Run(t, dir, "add", "m.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "main moves")
	return dir, svc
}

func TestPreviewAddListGetRenameRemove(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	p, err := svc.PreviewAdd(ctx, "feat/x", "main", "")
	if err != nil || p.Label != "feat/x → main" || p.ID == "" {
		t.Fatalf("add = %+v, %v", p, err)
	}
	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", ""); !errors.Is(err, ErrPreviewExists) {
		t.Fatalf("duplicate add = %v, want ErrPreviewExists", err)
	}
	if got, err := svc.PreviewGet(ctx, "feat/x → main"); err != nil || got.ID != p.ID {
		t.Fatalf("get by label = %+v, %v", got, err)
	}
	if err := svc.PreviewRename(ctx, p.ID, "login"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.PreviewGet(ctx, "login"); got.ID != p.ID {
		t.Fatal("get by new label failed")
	}
	if err := svc.PreviewRemove(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewGet(ctx, p.ID); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("get after remove = %v", err)
	}
	if l, _ := svc.PreviewList(ctx); len(l) != 0 {
		t.Fatalf("list after remove = %v", l)
	}
}

func TestPreviewAddRefusesUnknownBranchAndSamePair(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "nope", "main", ""); err == nil || !contains(err.Error(), "nope") {
		t.Fatalf("unknown source must be refused naming it, got %v", err)
	}
	if _, err := svc.PreviewAdd(ctx, "main", "main", ""); err == nil {
		t.Fatal("source == target must be refused")
	}
}

func TestPreviewsDisabledSeamAndUsePreviewsDir(t *testing.T) {
	prev := PreviewsDisabled
	PreviewsDisabled = true
	defer func() { PreviewsDisabled = prev }()
	ctx := context.Background()
	off := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	if _, err := off.PreviewList(ctx); !errors.Is(err, ErrPreviewsDisabled) {
		t.Fatalf("disabled list = %v", err)
	}
	_, on := previewRepo(t) // UsePreviewsDir outranks the global switch
	if _, err := on.PreviewList(ctx); err != nil {
		t.Fatalf("UsePreviewsDir must override PreviewsDisabled: %v", err)
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && index(s, sub) >= 0) }
func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
