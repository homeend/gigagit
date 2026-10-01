package domain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/sessionreg"
	"github.com/homeend/gigagit/internal/wtclaim"
)

func TestClaimAndRelease(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "c")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, Agent: "claude", State: "running"}}})
	ctx := context.Background()
	if err := svc.ClaimWorktree(ctx, wt, "", "", pol()); !errors.Is(err, ErrNotAgent) {
		t.Fatalf("no session = %v", err)
	}
	if err := svc.ClaimWorktree(ctx, wt, "p/s1", "https://x/1", pol()); err != nil {
		t.Fatal(err)
	}
	var nf *NotFreeError
	if err := svc.ClaimWorktree(ctx, wt, "p/s1", "", pol()); !errors.As(err, &nf) || !slices.Contains(nf.BlockedBy, "claimed") {
		t.Fatalf("second claim = %v", err)
	}
	infos, _ := svc.WorktreeInventory(ctx, pol(), false)
	if w := find(t, infos, wt); w.Claim() == nil || w.Claim().Note != "https://x/1" || w.Claim().Agent != "claude" {
		t.Fatalf("claim = %+v (agent must come from the registry entry)", w.Claim())
	}
	if _, err := svc.ReleaseWorktree(ctx, wt, "p/s9", false); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("non-holder release = %v", err)
	}
	if ok, err := svc.ReleaseWorktree(ctx, wt, "p/s1", false); err != nil || !ok {
		t.Fatalf("release = %v %v", ok, err)
	}
	if ok, _ := svc.ReleaseWorktree(ctx, wt, "p/s1", false); ok {
		t.Fatal("second release must report no claim")
	}
}

func TestClaimSweepsClaimOfDeadRegistry(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "d")
	sessionreg.Write(reg, "dead", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "dead/s1", Dir: main, State: "running"}}})
	ctx := context.Background()
	if err := svc.ClaimWorktree(ctx, wt, "dead/s1", "", pol()); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * sessionreg.LiveWindow)
	os.Chtimes(filepath.Join(reg, "dead.json"), past, past)
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 2, Sessions: []sessionreg.Entry{{ID: "p/s2", Dir: main, State: "running"}}})
	if err := svc.ClaimWorktree(ctx, wt, "p/s2", "", pol()); err != nil {
		t.Fatalf("claim over a dead claim = %v", err)
	}
}

func TestStalledRegistryKeepsClaim(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "stall")
	proc := fmt.Sprintf("%d-1", os.Getpid()) // a live process, not this one's ProcTag
	sessionreg.Write(reg, proc, sessionreg.Registry{PID: os.Getpid(), Sessions: []sessionreg.Entry{{ID: proc + "/s1", Dir: main, State: "running"}}})
	ctx := context.Background()
	if err := svc.ClaimWorktree(ctx, wt, proc+"/s1", "", pol()); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * sessionreg.LiveWindow)
	os.Chtimes(filepath.Join(reg, proc+".json"), past, past) // the TUI froze
	infos, _ := svc.WorktreeInventory(ctx, pol(), false)
	if w := find(t, infos, wt); w.Claim() == nil {
		t.Fatal("a stalled registry of a LIVE process must keep its claim")
	}
}

func TestClaimRefusesDeadSession(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "nolive")
	var nl *SessionNotLiveError
	if err := svc.ClaimWorktree(context.Background(), wt, "0-1/s1", "", pol()); !errors.As(err, &nl) {
		t.Fatalf("claim by a dead session = %v", err)
	}
	if _, err := os.Stat(filepath.Join(git.GitDirAt(wt), wtclaim.FileName)); !os.IsNotExist(err) {
		t.Fatal("no claim file may be written")
	}
}

func TestClaimRefusesMissingWorktree(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "gone")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "running"}}})
	os.RemoveAll(wt)
	var nf *NotFreeError
	if err := svc.ClaimWorktree(context.Background(), wt, "p/s1", "", pol()); !errors.As(err, &nf) || !slices.Contains(nf.BlockedBy, "missing") {
		t.Fatalf("claim on a missing worktree = %v", err)
	}
	if _, err := os.Stat(wtclaim.FileName); !os.IsNotExist(err) {
		t.Fatal("gg-claim written into the cwd")
	}
}

func TestEmptyClaimFileDiesAfterGrace(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "empty")
	f := filepath.Join(git.GitDirAt(wt), wtclaim.FileName)
	os.WriteFile(f, nil, 0o644)
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), false)
	if find(t, infos, wt).Claim() == nil {
		t.Fatal("a fresh empty claim is mid-write: alive")
	}
	past := time.Now().Add(-time.Minute)
	os.Chtimes(f, past, past)
	infos, _ = svc.WorktreeInventory(context.Background(), pol(), false)
	if find(t, infos, wt).Claim() != nil {
		t.Fatal("an old empty claim is a crashed claimer's: dead")
	}
}

func TestUnparsableClaimDiesAfterGrace(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "garbled")
	f := filepath.Join(git.GitDirAt(wt), wtclaim.FileName)
	os.WriteFile(f, []byte("not = [toml"), 0o644)
	past := time.Now().Add(-time.Minute)
	os.Chtimes(f, past, past)
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "running"}}})
	if err := svc.ClaimWorktree(context.Background(), wt, "p/s1", "", pol()); err != nil {
		t.Fatalf("claim over an old garbled claim = %v", err)
	}
}

func TestAgentsConfigAnchoredOnMain(t *testing.T) {
	t.Parallel()
	main, _, _ := inventoryRepo(t)
	wt := addWT(t, main, "lnk")
	os.WriteFile(filepath.Join(main, ".gg.toml"), []byte("[agents]\nreserved = [\"x\"]\n"), 0o644)
	os.WriteFile(filepath.Join(wt, ".gg.toml"), []byte("[agents]\nreserved = [\"y\"]\n"), 0o644)
	linked := New(&git.Repo{Runner: gitexec.NewExecRunner("git", wt, observ.NewRing(50))})
	linked.UseSessionRegistryDir(t.TempDir())
	ac, path, err := linked.AgentsConfig(context.Background())
	if err != nil || !SameCheckout(filepath.Dir(path), main) || !slices.Equal(ac.Reserved, []string{"x"}) {
		t.Fatalf("AgentsConfig from a linked worktree = %+v %q %v", ac, path, err)
	}
}

func TestExitedSessionClaimIsDead(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "e")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "running"}}})
	ctx := context.Background()
	svc.ClaimWorktree(ctx, wt, "p/s1", "", pol())
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "exited"}}})
	infos, _ := svc.WorktreeInventory(ctx, pol(), false)
	if w := find(t, infos, wt); w.Claim() != nil || !w.Free {
		t.Fatalf("exited-session claim must be dead: %+v", w)
	}
	if _, err := os.Stat(filepath.Join(git.GitDirAt(wt), wtclaim.FileName)); !os.IsNotExist(err) {
		t.Fatal("dead claim not swept")
	}
}

func TestReleaseForce(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "f")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "running"}}})
	ctx := context.Background()
	svc.ClaimWorktree(ctx, wt, "p/s1", "", pol())
	if ok, err := svc.ReleaseWorktree(ctx, wt, "", true); err != nil || !ok {
		t.Fatalf("force release = %v %v", ok, err)
	}
}

func TestWorktreeMarksAndReserve(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "m")
	cfg := filepath.Join(main, ".gg.toml")
	ctx := context.Background()
	if err := svc.SetWorktreeReserved(ctx, cfg, nil, wt, true); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load("", cfg)
	rel, _ := filepath.Rel(main, wt)
	if !slices.Equal(c.Agents.Reserved, []string{filepath.ToSlash(rel)}) {
		t.Fatalf("reserved = %q, want relative %q", c.Agents.Reserved, rel)
	}
	wts, _ := svc.Worktrees(ctx)
	marks := svc.WorktreeMarks(wts, c.Agents.Reserved)
	reservedSeen := false
	for p, mk := range marks {
		if SameCheckout(p, wt) && mk.Reserved {
			reservedSeen = true
		}
	}
	if !reservedSeen {
		t.Fatalf("marks = %+v", marks)
	}
	if err := svc.SetWorktreeReserved(ctx, cfg, c.Agents.Reserved, wt, false); err != nil {
		t.Fatal(err)
	}
	c, _ = config.Load("", cfg)
	if len(c.Agents.Reserved) != 0 {
		t.Fatalf("reserved after unreserve = %q", c.Agents.Reserved)
	}
}

// TestRecycleThroughExecuteAsksTheGuards proves the OpDeps.Guards wiring and
// that GuardReport never waits on the reservation the op itself holds (a
// gated read inside the op would deadlock until the context expired).
func TestRecycleThroughExecuteAsksTheGuards(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "res")
	if out, err := exec.Command("git", "-C", main, "branch", "loose").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	cfg := filepath.Join(main, ".gg.toml")
	if err := svc.SetWorktreeReserved(context.Background(), cfg, nil, wt, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := svc.Execute(ctx, engine.RecycleWorktree{Dir: wt, Branch: "loose"}, nil,
		engine.MapDecider{engine.RecycleBlockedDecisionID: "abort"})
	if err != nil || !strings.Contains(res.Summary, "cancelled") {
		t.Fatalf("recycle of a reserved worktree = %+v, %v (a timeout means GuardReport waited on the gate)", res, err)
	}
}
