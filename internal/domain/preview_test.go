package domain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
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

func TestPreviewSummaryStates(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	sum, err := svc.PreviewSummary(ctx, "feat/x", "main")
	if err != nil || sum.State != PreviewOK || sum.Files != 2 || sum.Ahead != 2 {
		t.Fatalf("ok pair = %+v, %v; want 2 files, 2 ahead", sum, err)
	}
	if len(sum.SourceHash) != 40 || len(sum.TargetHash) != 40 {
		t.Fatalf("hashes must be full shas: %+v", sum)
	}
	// Reverse: main brings m.txt into feat/x.
	if rev, _ := svc.PreviewSummary(ctx, "main", "feat/x"); rev.Files != 1 || rev.Ahead != 1 {
		t.Fatalf("reverse = %+v", rev)
	}
	if m, _ := svc.PreviewSummary(ctx, "nope", "main"); m.State != PreviewMissingSource {
		t.Fatalf("missing source = %+v", m)
	}
	if m, _ := svc.PreviewSummary(ctx, "main", "nope"); m.State != PreviewMissingTarget {
		t.Fatalf("missing target = %+v", m)
	}
	gittest.Run(t, dir, "merge", "-q", "--no-edit", "feat/x") // main now contains feat/x
	if m, _ := svc.PreviewSummary(ctx, "feat/x", "main"); m.State != PreviewMerged || m.Ahead != 0 {
		t.Fatalf("merged = %+v", m)
	}
	gittest.Run(t, dir, "checkout", "-q", "--orphan", "lonely")
	gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "unrelated root")
	if m, _ := svc.PreviewSummary(ctx, "lonely", "main"); m.State != PreviewNoBase {
		t.Fatalf("no base = %+v", m)
	}
}

// callCount counts f's recorded calls with the given span name (FakeRunner has
// no counter method; Calls is the only record of invocations).
func callCount(f *gitexec.FakeRunner, name string) int {
	n := 0
	for _, c := range f.Calls {
		if c.Name == name {
			n++
		}
	}
	return n
}

func TestPreviewSummaryCachedByHashPair(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse verify commit (resolve)", gitexec.Result{Stdout: "1111111111111111111111111111111111111111\n"})
	f.SetResponse("git merge-base", gitexec.Result{Stdout: "2222222222222222222222222222222222222222\n"})
	f.SetResponse("git rev-list --left-right --count", gitexec.Result{Stdout: "1\t3\n"})
	f.SetResponse("git diff --name-only (range)", gitexec.Result{Stdout: "a\x00b\x00"})
	svc := New(&git.Repo{Runner: f})
	ctx := context.Background()
	first, err := svc.PreviewSummary(ctx, "feat", "main")
	if err != nil || first.Ahead != 3 || first.Files != 2 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	n := callCount(f, "git rev-list --left-right --count")
	nBase := callCount(f, "git merge-base")
	nDiff := callCount(f, "git diff --name-only (range)")
	if _, err := svc.PreviewSummary(ctx, "feat", "main"); err != nil {
		t.Fatal(err)
	}
	if callCount(f, "git rev-list --left-right --count") != n {
		t.Fatal("unchanged tips must be served from the cache (no rev-list call)")
	}
	if callCount(f, "git merge-base") != nBase {
		t.Fatal("unchanged tips must be served from the cache (no merge-base call)")
	}
	if callCount(f, "git diff --name-only (range)") != nDiff {
		t.Fatal("unchanged tips must be served from the cache (no diff --name-only call)")
	}
}

// TestPreviewSummaryMergeBaseCancellationNotCached: a context cancellation
// mid-merge-base must propagate as an error, NOT be classified as
// PreviewNoBase and cached under the hash-pair key — a cancelled attempt must
// never poison a pair that does have a common base.
func TestPreviewSummaryMergeBaseCancellationNotCached(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse verify commit (resolve)", gitexec.Result{Stdout: "3333333333333333333333333333333333333333\n"})
	ctx, cancel := context.WithCancel(context.Background())
	first := true
	f.SetHandler("git merge-base", func(c context.Context, argv []string) (gitexec.Result, error) {
		if first {
			first = false
			cancel()
			return gitexec.Result{}, errors.New("git merge-base cancelled: context canceled")
		}
		return gitexec.Result{Stdout: "4444444444444444444444444444444444444444\n"}, nil
	})
	svc := New(&git.Repo{Runner: f})
	if _, err := svc.PreviewSummary(ctx, "feat", "main"); err == nil {
		t.Fatal("a cancelled merge-base must propagate an error, not a PreviewNoBase result")
	}
	// A fresh, uncancelled attempt for the SAME pair must recompute (not read a
	// poisoned cache entry) and reach PreviewOK.
	f.SetResponse("git rev-list --left-right --count", gitexec.Result{Stdout: "0\t1\n"})
	f.SetResponse("git diff --name-only (range)", gitexec.Result{Stdout: "a\x00"})
	second, err := svc.PreviewSummary(context.Background(), "feat", "main")
	if err != nil || second.State != PreviewOK {
		t.Fatalf("second (uncancelled) attempt = %+v, %v; want PreviewOK (nothing cached by the cancelled attempt)", second, err)
	}
}

// mergeBaseOf shells out to a real git merge-base (there is no gittest.Output
// helper), mirroring headHash's pattern in compare_test.go.
func mergeBaseOf(t *testing.T, dir, a, b string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "merge-base", a, b)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1]) // strip newline
}

func TestPreviewOpenEndpoints(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	eps, err := svc.PreviewOpen(ctx, "feat/x", "main")
	if err != nil || eps.Summary.State != PreviewOK {
		t.Fatalf("open = %+v, %v", eps, err)
	}
	base := mergeBaseOf(t, dir, "main", "feat/x")
	if eps.Left != (model.Endpoint{Kind: model.EndpointCommit, Hash: base}) || eps.Right.Hash != eps.Summary.SourceHash {
		t.Fatalf("endpoints = %+v, want left=merge-base %s right=source tip", eps, base)
	}
	files, _ := svc.CompareFiles(ctx, eps.Left, eps.Right)
	if len(files) != 2 || files[0].Path != "a.txt" {
		t.Fatalf("compare over the endpoints = %+v, want a.txt b.txt only (never m.txt)", files)
	}
	if eps, _ := svc.PreviewOpen(ctx, "nope", "main"); eps.Summary.State != PreviewMissingSource || eps.Left != (model.Endpoint{}) {
		t.Fatalf("missing side must yield zero endpoints: %+v", eps)
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
