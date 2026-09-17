// Package linknav turns a gg:// link into the place a gg session lands on:
// the navigate steer.Command `gg session navigate <link>`, `gg open <link>`
// and the TUI's paste field all post for it, and the fully-resolved link the
// TUI's --at startup gate consumes. One builder, three consumers, so a link
// can never mean different places on different surfaces.
//
// It sits between domain and the frontends: domain must not import steer (the
// resolver's LiveFn seam exists for that), and the TUI must not import cli.
package linknav

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// The one link-shape refusal a navigate can hit. It is a CALLER mistake
// (exit 2 on the CLI), unlike a git failure (exit 1), and every consumer maps
// it the same way.
var ErrRepoOnly = errors.New("that link names a repository, not a place in it")

// Opts wires the resolver to this process: the MRU registry, the cwd's
// service, and the steer-presence probe.
func Opts(registryPath string, cwd *domain.Service) domain.ResolveOpts {
	return domain.ResolveOpts{
		RegistryPath: registryPath,
		Cwd:          cwd,
		LiveFn: func(commonDir, checkout string) bool {
			dir := config.SessionSteerDir(commonDir, checkout)
			if dir == "" {
				return false
			}
			if _, ok := steer.Live(dir, steer.TUIPresence); ok {
				return true
			}
			_, ok := steer.Live(dir, steer.WebPresence)
			return ok
		},
	}
}

// Resolve parses and resolves link text against this machine (Opts).
func Resolve(ctx context.Context, registryPath string, cwd *domain.Service, s string) (domain.Resolved, error) {
	l, err := model.ParseLink(s)
	if err != nil {
		return domain.Resolved{}, err
	}
	return domain.ResolveLink(ctx, l, Opts(registryPath, cwd))
}

// RepoOnly reports whether res names a checkout and nothing in it — no path,
// no commit, no preview. Such a link has no place to navigate to; opening it
// means opening gg in that checkout.
func RepoOnly(res domain.Resolved) bool {
	return res.Addr.Path == "" && res.Commit == "" && res.Preview == nil
}

// TargetOf is the wire target a file address names.
func TargetOf(a model.FileAddress) *steer.Target {
	t := &steer.Target{State: "unstaged"}
	switch a.State {
	case model.StateStaged:
		t.State = "staged"
	case model.StateUntracked:
		t.State = "untracked"
	case model.StateCommitted:
		t.State, t.Commit = "commit", a.Commit
	}
	return t
}

// HunkLine turns hunk N into the {side,no} the consumer lands on: the FIRST
// line of the range HunkRange resolves — the hunk's new span, or its old span
// for a pure deletion. (A NOTE anchors at a range END; a landing wants the top
// of the region.) It is the SAME HunkRange `gg note add --hunk N` uses, so a
// hunk number an agent read from `gg diff --hunks` addresses one region for
// both verbs. Resolved HERE and never in the TUI's converter, so one landing
// path serves --hunk, --new-line and --old-line alike.
func HunkLine(ctx context.Context, svc *domain.Service, cached bool, rev, path string, n int) (steer.Line, error) {
	spec, err := svc.HunkDiffSpec(ctx, cached, rev, []string{path})
	if err != nil {
		return steer.Line{}, err
	}
	side, rng, err := svc.HunkRange(ctx, spec, path, n)
	if err != nil {
		return steer.Line{}, err
	}
	if side == model.NoteSideOld {
		return steer.Line{Side: "old", No: rng[0]}, nil
	}
	return steer.Line{Side: "new", No: rng[0]}, nil
}

// Command builds the navigate command a RESOLVED link names. svc must be the
// service for the link's OWN checkout — every git question (hunk lowering)
// is asked there, wherever the caller ran.
func Command(ctx context.Context, svc *domain.Service, res domain.Resolved) (steer.Command, error) {
	c := steer.Command{Cmd: "navigate"}
	if res.Preview != nil {
		// The PAIR rides the wire, never the tip: the consumer resolves the tip
		// itself, so a tip that moved between post and apply is honoured.
		c.Target = &steer.Target{State: "preview", Source: res.Preview.Source, Target: res.Preview.Target}
		if res.Addr.Path == "" {
			return c, nil // reveal the Previews entry
		}
		c.File = res.Addr.Path
		line := steer.Line{Side: string(res.Side), No: res.Line}
		if res.Hunk > 0 {
			// PreviewHunkAnchor, never HunkLine: the numbering is the PREVIEW's
			// patch (merge-base → tip), and a delete-only hunk has no new side
			// to land on.
			side, rng, err := svc.PreviewHunkAnchor(ctx, *res.Preview, res.Addr.Path, res.Hunk)
			if err != nil {
				return steer.Command{}, err
			}
			line = steer.Line{Side: string(side), No: rng[0]}
		}
		// A link with no line is a link to the FILE: the consumer opens its diff
		// and leaves the cursor where it was (steer.Command.Line's contract). It
		// used to be ErrNoLine, which made `gg open` refuse the very link
		// `gg link <path>` prints — the producer and the navigator disagreeing
		// about what a valid link is.
		if line.No > 0 {
			c.Line = &line
		}
		return c, nil
	}
	if res.Ref != "" {
		// The NAME rides the wire, never the tip: the consumer resolves it
		// itself, so a tip that moved between post and apply is honoured —
		// the rule the preview arm above already follows.
		c.Target = &steer.Target{State: "ref", Ref: res.Ref}
		if res.Addr.Path == "" {
			return c, nil // reveal the tree at that tip
		}
		c.File = res.Addr.Path
		// A ref is a POINT, so its file diff is the commit lane's: lower a
		// hunk against the resolved tip, exactly as the commit arm does.
		line := steer.Line{Side: string(res.Side), No: res.Line}
		if res.Hunk > 0 {
			l, err := HunkLine(ctx, svc, res.Addr.State == model.StateStaged, res.Commit, res.Addr.Path, res.Hunk)
			if err != nil {
				return steer.Command{}, err
			}
			line = l
		}
		if line.No > 0 {
			c.Line = &line
		}
		return c, nil
	}
	if p := res.Pair; p != nil {
		// The two halves ride the wire as NAMES, exactly like the ref arm
		// above: the consumer resolves them itself.
		c.Target = &steer.Target{State: "pair", A: p.A, B: p.B}
		if res.Addr.Path == "" {
			return c, nil // open the compare
		}
		c.File = res.Addr.Path
		// A pair's file diff is against its newer end B — res.Commit already
		// holds it — exactly as the ref arm lowers against its tip.
		line := steer.Line{Side: string(res.Side), No: res.Line}
		if res.Hunk > 0 {
			l, err := HunkLine(ctx, svc, res.Addr.State == model.StateStaged, res.Commit, res.Addr.Path, res.Hunk)
			if err != nil {
				return steer.Command{}, err
			}
			line = l
		}
		if line.No > 0 {
			c.Line = &line
		}
		return c, nil
	}
	if res.Addr.Path == "" {
		// A link with no path reveals the commit (spec §1).
		if res.Commit == "" {
			return steer.Command{}, ErrRepoOnly
		}
		c.Commit = res.Commit
		return c, nil
	}
	c.File, c.Target = res.Addr.Path, TargetOf(res.Addr)
	line := steer.Line{Side: string(res.Side), No: res.Line}
	if res.Hunk > 0 {
		// No StateUntracked guard here: the grammar has no untracked target, so
		// ParseLink (the only source of a Resolved) never produces one — an
		// untracked file's link is the plain working-tree form, whose
		// index→file diff has hunks.
		l, err := HunkLine(ctx, svc, res.Addr.State == model.StateStaged, res.Addr.Commit, res.Addr.Path, res.Hunk)
		if err != nil {
			return steer.Command{}, err
		}
		line = l
	}
	// A link with no line is a link to the FILE: the consumer opens its diff
	// and leaves the cursor where it was (steer.Command.Line's contract). It
	// used to be ErrNoLine, which made `gg open` refuse the very link
	// `gg link <path>` prints — the producer and the navigator disagreeing
	// about what a valid link is.
	if line.No > 0 {
		c.Line = &line
	}
	return c, nil
}

// AtLink is the link handed to a TUI launcher (`gg open`, or the paste
// field's repo switch): the same place, fully resolved, with any #<hunk>
// ALREADY lowered to the line Command computed. The TUI's converter
// (steerCommandForLink) is pure, so everything needing a repository must be
// settled here.
func AtLink(res domain.Resolved, c steer.Command) model.Link {
	l := model.Link{
		Repo: model.LinkRepo{Abs: filepath.ToSlash(filepath.Clean(res.Checkout))},
		Path: res.Addr.Path,
		Side: model.NoteSideNew,
	}
	switch {
	case res.Preview != nil:
		l.Target = model.LinkTarget{
			State:   model.StateCommitted,
			Preview: &model.LinkPreview{Source: res.Preview.Source, Target: res.Preview.Target},
		}
	case res.Ref != "":
		l.Target = model.LinkTarget{State: model.StateCommitted, Ref: res.Ref}
	case res.Pair != nil:
		l.Target = model.LinkTarget{State: model.StateCommitted, Pair: res.Pair}
	default:
		l.Target = model.LinkTarget{State: res.Addr.State, Commit: res.Addr.Commit}
	}
	// A preview link never carries Side old — Command already refused an
	// old-side preview hunk (domain.ErrPreviewOldSide) before c reached
	// here, so this is a no-op for one; every other shape may.
	if c.Line != nil && c.Line.Side == "old" {
		l.Side = model.NoteSideOld
	}
	if c.Line != nil {
		l.Line = c.Line.No
	}
	return l
}
