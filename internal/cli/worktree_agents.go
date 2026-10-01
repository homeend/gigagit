package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// agentsConfig is the domain's main-anchored [agents] view (one source for
// the CLI and the TUI).
func agentsConfig(svc *domain.Service) (config.AgentsConfig, string, error) {
	return svc.AgentsConfig(context.Background())
}

// toJSON: the fixed identity fields, then every guard's fact under its own
// key (dirty, claim, sessions, tui, reserved, paused_op, git_lock) — a new
// guard in the composition shows up here with no CLI change.
func toJSON(w domain.WorktreeInfo) map[string]any {
	out := map[string]any{
		"path": w.Path, "branch": w.Branch, "head": w.Head,
		"main": w.Main, "detached": w.Detached,
		"free": w.Free, "blocked_by": w.BlockedBy, "recycle": nil,
	}
	if w.BlockedBy == nil {
		out["blocked_by"] = []string{}
	}
	if w.Recycle != "" {
		out["recycle"] = w.Recycle
	}
	for k, v := range w.Facts {
		out[k] = v
	}
	return out
}

func cmdWorktreeList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "one JSON object per worktree with the facts an orchestrating agent needs")
	freeOnly := fs.Bool("free", false, "only worktrees an agent may take, best first")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	if !*asJSON && !*freeOnly {
		wts, err := svc.Worktrees(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		for _, w := range wts {
			printWorktreeLine(stdout, w.Branch, w.Path)
		}
		return 0
	}
	ac, _, err := agentsConfig(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	pol := domain.PolicyFromConfig(ac)
	warnStaleAfter(stderr, pol)
	infos, err := svc.WorktreeInventory(ctx, pol, *freeOnly)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if *asJSON {
		out := make([]map[string]any, 0, len(infos))
		for _, w := range infos {
			out = append(out, toJSON(w))
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	for _, w := range infos {
		printWorktreeLine(stdout, w.Branch, w.Path)
	}
	return 0
}

func printWorktreeLine(w io.Writer, branch, path string) {
	if branch == "" {
		branch = "(detached)"
	}
	fmt.Fprintf(w, "%s\t%s\n", branch, path)
}

func cmdWorktreeClaim(svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree claim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	note := fs.String("note", "", "why the worktree is taken (e.g. the issue URL); shown in the TUI")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg worktree claim [--note <text>] <path>")
		return 2
	}
	sid := os.Getenv("GG_SESSION_ID")
	if sid == "" {
		fmt.Fprintln(stderr, "worktree claim: "+domain.ErrNotAgent.Error())
		return 2
	}
	path, code := resolveWorktreeArg(svc, workdir, fs.Arg(0), "claim", stderr)
	if code != 0 {
		return code
	}
	ac, _, err := agentsConfig(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	pol := domain.PolicyFromConfig(ac)
	warnStaleAfter(stderr, pol)
	if err := svc.ClaimWorktree(context.Background(), path, sid, *note, pol); err != nil {
		fmt.Fprintln(stderr, "worktree claim:", err)
		var nl *domain.SessionNotLiveError
		if errors.As(err, &nl) {
			return 2
		}
		return 1
	}
	fmt.Fprintf(stdout, "claimed %s\n", path)
	return 0
}

func cmdWorktreeRelease(svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "release a claim held by another session")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg worktree release [--force] <path>")
		return 2
	}
	path, code := resolveWorktreeArg(svc, workdir, fs.Arg(0), "release", stderr)
	if code != 0 {
		return code
	}
	ok, err := svc.ReleaseWorktree(context.Background(), path, os.Getenv("GG_SESSION_ID"), *force)
	if err != nil {
		fmt.Fprintln(stderr, "worktree release:", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(stdout, "no claim on %s\n", path)
		return 0
	}
	fmt.Fprintf(stdout, "released %s\n", path)
	return 0
}

func cmdWorktreeReserve(svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer, on bool) int {
	verb := map[bool]string{true: "reserve", false: "unreserve"}[on]
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: gg worktree %s <path>\n", verb)
		return 2
	}
	path, code := resolveWorktreeArg(svc, workdir, args[0], verb, stderr)
	if code != 0 {
		return code
	}
	ac, cfgPath, err := agentsConfig(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := svc.SetWorktreeReserved(context.Background(), cfgPath, ac.Reserved, path, on); err != nil {
		fmt.Fprintf(stderr, "worktree %s: %v\n", verb, err)
		return 1
	}
	fmt.Fprintf(stdout, "%sd %s\n", verb, path)
	return 0
}

// resolveWorktreeArg maps a user/agent-typed path to the listed worktree. A
// relative arg is taken against workdir (the CLI's "here"), never the
// process cwd — matchWorktreeArg's own filepath.Abs uses the latter.
func resolveWorktreeArg(svc *domain.Service, workdir, arg, verb string, stderr io.Writer) (string, int) {
	wts, err := svc.Worktrees(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return "", 1
	}
	if !filepath.IsAbs(arg) {
		arg = filepath.Join(workdir, arg)
	}
	m := matchWorktreeArg(wts, filepath.Clean(arg))
	if m == nil {
		fmt.Fprintf(stderr, "worktree %s: no worktree at %q\n", verb, arg)
		return "", 1
	}
	return m.Path, 0
}

// warnStaleAfter names a stale_after typo: it holds back every dirty worktree.
func warnStaleAfter(stderr io.Writer, pol domain.InventoryPolicy) {
	if pol.StaleErr != "" {
		fmt.Fprintln(stderr, "warning:", pol.StaleErr, "— every dirty worktree counts as recently changed")
	}
}
