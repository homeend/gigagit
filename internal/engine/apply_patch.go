package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/homeend/gigagit/internal/git"
)

// ApplyPatchMode selects how ApplyPatch lands the patch.
type ApplyPatchMode int

const (
	// ApplyModeAuto detects the format: a mailbox forks via the
	// apply_patch.mode decision, a plain diff goes to the working tree.
	ApplyModeAuto ApplyPatchMode = iota
	// ApplyModeWorkingTree runs `git apply --3way`: changes land unstaged;
	// conflicts land as markers + unmerged entries for the conflict process.
	ApplyModeWorkingTree
	// ApplyModeCommits runs `git am --3way`: the mailbox is replayed as real
	// commits (author/date/message preserved). Atomic: any failure rolls back
	// via `git am --abort`. Mailbox patches only.
	ApplyModeCommits
)

// ApplyModeDecisionID names the mode fork a mailbox patch raises under
// ApplyModeAuto. Options: applyOptWorkingTree (safe, first), applyOptCommits.
const ApplyModeDecisionID = "apply_patch.mode"

const (
	applyOptWorkingTree = "working-tree"
	applyOptCommits     = "commits"
)

var (
	// ErrNotMailbox: ApplyModeCommits needs a format-patch mailbox — git am
	// has no author/message to work with on a bare diff.
	ErrNotMailbox = errors.New("not a format-patch mailbox; apply it to the working tree instead")
	// ErrApplyCancelled: the mode decision was cancelled; nothing ran.
	ErrApplyCancelled = errors.New("apply cancelled")
)

// ApplyPatch imports a patch file from disk (the inverse of the
// export-as-patch flow). It reads the file head via os directly — the
// read-side analog of ExportFile's outside-the-tree precedent (the patch may
// live anywhere). gg does NOT model a paused git am: the Commits path is
// all-or-nothing (AmInProgress-guarded AmAbort on failure), and the
// working-tree path surfaces conflicts through the ordinary status →
// conflict-process wiring (the SmartMerge keep-conflicts Result+error shape).
// Default TreeWrite reservation: both paths mutate the tree and/or refs.
type ApplyPatch struct {
	Path string
	Mode ApplyPatchMode
}

var _ Operation = ApplyPatch{}

func (op ApplyPatch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	head, err := readHead(op.Path, 4096)
	if err != nil {
		return Result{}, fmt.Errorf("read patch: %w", err)
	}
	if len(head) == 0 {
		return Result{}, fmt.Errorf("empty patch file: %s", op.Path)
	}
	mailbox := git.IsMailboxPatch(head)
	base := filepath.Base(op.Path)

	mode := op.Mode
	if mode == ApplyModeAuto {
		if !mailbox {
			mode = ApplyModeWorkingTree
		} else {
			choice, derr := deps.decide(ctx, DecisionRequest{
				ID:      ApplyModeDecisionID,
				Prompt:  base + " is a format-patch mailbox — apply how?",
				Options: []string{applyOptWorkingTree, applyOptCommits},
			})
			if derr != nil {
				return Result{}, derr
			}
			switch choice.Option {
			case applyOptWorkingTree:
				mode = ApplyModeWorkingTree
			case applyOptCommits:
				mode = ApplyModeCommits
			default:
				return Result{}, ErrApplyCancelled
			}
		}
	}
	if mode == ApplyModeCommits && !mailbox {
		return Result{}, ErrNotMailbox
	}

	if mode == ApplyModeCommits {
		return op.runAm(ctx, deps, base)
	}
	return op.runApply(ctx, deps, base)
}

// runAm replays the mailbox as commits, atomically: on any failure a started
// am is rolled back (guarded by AmInProgress — a bare rebase-apply dir
// belongs to a paused REBASE, which must not be am-aborted).
func (op ApplyPatch) runAm(ctx context.Context, deps OpDeps, base string) (Result, error) {
	deps.emit(ctx, Progress{Step: "applying", Detail: base + " (recreate commits)"})
	if amErr := deps.Repo.AmMailbox(ctx, op.Path, true); amErr != nil {
		if in, _ := deps.Repo.AmInProgress(ctx); in {
			if abortErr := deps.Repo.AmAbort(ctx); abortErr != nil {
				return Result{}, fmt.Errorf("patch does not apply cleanly (%v); git am --abort also failed: %w", amErr, abortErr)
			}
		}
		return Result{}, fmt.Errorf("patch does not apply cleanly; nothing changed: %w", amErr)
	}
	summary := "applied " + base + " as commits"
	// Name the resulting tip (the Commit op's read-back precedent;
	// best-effort — a failed read only costs the sha in the summary).
	if line, lerr := deps.Repo.CommitLine(ctx, "HEAD"); lerr == nil {
		summary = "applied " + base + ": now at " + line.Hash + " " + line.Subject
	}
	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// runApply lands the diff in the working tree. It tries a plain `git apply`
// first (threeWay=false): `--3way` implies `--index` in real git, so calling
// it unconditionally would stage even a clean apply, contradicting this
// mode's "lands unstaged" contract. Only on a miss does it retry with
// `--3way`, which exits non-zero BOTH when it left conflict markers and when
// it applied nothing — unmerged index entries tell the two apart.
//
// A clean --3way retry is the tricky case: --3way's implied --index means
// the retry, on success, staged the patch's changes — the SAME contract
// violation calling --3way unconditionally would cause, just reached via the
// fallback instead of the first attempt. So a clean fallback apply is
// followed by a surgical unstage: PatchPaths reads the patch (touching
// nothing) for exactly the paths it changed, and UnstagePaths restores only
// those from the index, leaving the resolved content in the working tree and
// any unrelated pre-staged user changes untouched. The conflict path below
// (a dirty --3way retry) is unaffected: it already returns Result+error and
// leaves the unmerged entries for the conflict process, which is correct.
func (op ApplyPatch) runApply(ctx context.Context, deps OpDeps, base string) (Result, error) {
	deps.emit(ctx, Progress{Step: "applying", Detail: base + " (working tree)"})
	applyErr := deps.Repo.ApplyPatch(ctx, op.Path, false)
	fellBackToThreeWay := applyErr != nil
	if fellBackToThreeWay {
		applyErr = deps.Repo.ApplyPatch(ctx, op.Path, true)
	}
	if applyErr == nil {
		if fellBackToThreeWay {
			if uerr := op.unstageAfterThreeWay(ctx, deps); uerr != nil {
				// The apply itself succeeded — the working tree genuinely
				// changed — so Changed:true here (like the keep-conflicts
				// shape below) rather than the zero Result, so a frontend
				// keying its refresh off Changed doesn't show stale state.
				return Result{Changed: true}, uerr
			}
		}
		res := Result{Summary: "applied " + base + " to working tree", Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	st, stErr := deps.Repo.Status(ctx)
	if stErr == nil && st.Counts().Conflicted > 0 {
		// The SmartMerge keep-conflicts shape: Result AND error, so the TUI
		// refreshes (conflict process picks the files up) and the CLI exits 1.
		n := st.Counts().Conflicted
		return Result{Summary: "applied " + base + " with conflicts in " + strconv.Itoa(n) + " file(s) (left in tree)", Changed: true},
			fmt.Errorf("apply conflict: %s left %d file(s) unmerged — resolve and commit", base, n)
	}
	return Result{}, fmt.Errorf("patch does not apply; nothing changed: %w", applyErr)
}

// unstageAfterThreeWay restores the index after a clean --3way fallback
// apply (see runApply's doc comment for why --3way's implied --index makes
// this necessary). It reads back exactly the paths the patch touched via
// PatchPaths (a read of the patch file only, nothing git-side) and unstages
// only those, so an unrelated file the caller had already staged before
// running this op is left alone. Any failure here means the working tree
// carries the patch's changes but the index is in an unknown state relative
// to those paths — the error is worded so callers don't mistake it for a
// failed apply (the apply itself already succeeded). Known gap (see
// git.PatchPaths' doc comment): for a renamed file, PatchPaths can only
// report the new path, so the old path's staged deletion survives — a
// smaller residual state, not the original silent-full-stage bug.
func (op ApplyPatch) unstageAfterThreeWay(ctx context.Context, deps OpDeps) error {
	paths, perr := deps.Repo.PatchPaths(ctx, op.Path)
	if perr != nil {
		return fmt.Errorf("applied but left staged: read patch paths: %w", perr)
	}
	if len(paths) == 0 {
		return fmt.Errorf("applied but left staged: patch reported no touched paths")
	}
	if perr := deps.Repo.UnstagePaths(ctx, paths); perr != nil {
		return fmt.Errorf("applied but left staged: %w", perr)
	}
	return nil
}

// readHead returns up to n bytes from the start of the file at path.
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:read], nil
}
