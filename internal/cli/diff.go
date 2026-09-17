package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// cmdDiff implements `gg diff [--stat|--name-only|--hunks [--json]] [--cached] [<rev>] [-- <paths>]`.
// Default prints the full patch; --stat prints terse "path +A -D" lines with
// an "N files +A -D" trailer; --name-only prints bare paths; --hunks lists
// each file's numbered git @@ hunks (optionally as JSON via --json), the
// same numbering `gg note add --hunk N` resolves against. Paths must follow
// a "--" separator so a rev is never ambiguous with a path.
func cmdDiff(svc *domain.Service, dir string, args []string, stdout, stderr io.Writer) int {
	head, paths := splitDashDash(args)
	// A positional starting with gg:// is a LINK, never a rev (the whole rule —
	// no heuristic). Peel a LEADING one off before flag.Parse: flag.Parse stops
	// at the first non-flag argument, so a link followed by flags (`gg diff
	// <link> --hunks --json`) would otherwise strand those flags as extra
	// positionals instead of being parsed (mirrors cmdNote's link-must-be-
	// first-argument rule).
	link := ""
	if len(head) > 0 && isLinkArg(head[0]) {
		link, head = head[0], head[1:]
	}
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stat := fs.Bool("stat", false, "terse per-file change counts")
	nameOnly := fs.Bool("name-only", false, "changed paths only")
	cached := fs.Bool("cached", false, "diff the index against HEAD")
	hunks := fs.Bool("hunks", false, "list each file's numbered git @@ hunks instead of the patch")
	asJSON := fs.Bool("json", false, "with --hunks: emit the hunk list as JSON")
	pf := addPreviewFlag(fs)
	if err := fs.Parse(head); err != nil {
		return 2
	}
	if *stat && *nameOnly {
		fmt.Fprintln(stderr, "diff: --stat and --name-only are mutually exclusive")
		return 2
	}
	if *hunks && (*stat || *nameOnly) {
		fmt.Fprintln(stderr, "diff: --hunks is mutually exclusive with --stat/--name-only")
		return 2
	}
	if *asJSON && !*hunks {
		fmt.Fprintln(stderr, "diff: --json requires --hunks")
		return 2
	}
	posCount := fs.NArg()
	if link != "" {
		posCount++
	}
	if posCount > 1 {
		fmt.Fprintln(stderr, "usage: gg diff [--stat|--name-only|--hunks [--json]] [--cached] [<rev>|<A..B>] [-- <paths>...]")
		return 2
	}
	rev := link
	if rev == "" && fs.NArg() == 1 {
		rev = fs.Arg(0)
	}
	if pf.set() {
		// Ruling 9: --preview is sugar over the existing A...B range path —
		// resolve the pair, then run the code every other range runs.
		if *cached || rev != "" {
			return previewUsageErr("diff", stderr)
		}
		ctx := context.Background()
		tgt, err := resolvePreviewTarget(ctx, svc, *pf.spec)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return renderDiffSpec(ctx, svc, tgt.withPaths(paths), *hunks, *asJSON, *stat, *nameOnly, stdout, stderr)
	}
	// It carries the path and the target, so combining it with the flags it
	// replaces is a usage error rather than a silent override.
	if isLinkArg(rev) {
		if *cached || len(paths) > 0 {
			fmt.Fprintln(stderr, "diff: a gg:// link already names the file and the target; drop --cached and the -- <paths>")
			return 2
		}
		ctx := context.Background()
		// diff opts UP for a pair: unlike show/note/highlight it needs no
		// single commit to anchor on, only a range to diff (ruling R4).
		res, err := resolveLinkArgShapes(ctx, svc, rev, linkShapes{Ref: true, Pair: true}, "diff")
		if err != nil {
			return linkExit("diff", err, stderr)
		}
		// Run against the checkout the link named, wherever the cwd is.
		svc = openLinkTarget(res)
		spec, err := linkDiffSpec(ctx, svc, res)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return renderDiffSpec(ctx, svc, spec, *hunks, *asJSON, *stat, *nameOnly, stdout, stderr)
	}
	if *hunks && *cached && rev != "" && !strings.Contains(rev, "..") {
		fmt.Fprintln(stderr, "diff: --cached cannot be combined with a commit under --hunks (staged hunks are HEAD→index; a commit's hunks are its own change)")
		return 2
	}
	ctx := context.Background()
	var spec model.DiffSpec
	if *hunks {
		// --hunks numbers the patch a NOTE anchors to, so a bare commit means
		// that commit's own change (parent → commit; a root commit diffs
		// against the empty tree), not `git diff <c>`. HunkDiffSpec is the
		// single source of that rule, shared with `gg note add --hunk N`.
		s, err := svc.HunkDiffSpec(ctx, *cached, rev, repoPathspecs(svc, dir, paths))
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		spec = s
	} else {
		spec = model.DiffSpec{Cached: *cached, Rev: rev, Paths: repoPathspecs(svc, dir, paths)}
	}
	return renderDiffSpec(ctx, svc, spec, *hunks, *asJSON, *stat, *nameOnly, stdout, stderr)
}

// renderDiffSpec prints spec in whichever mode the flags chose. Shared by the
// flag path and the gg:// link path so both render identically.
func renderDiffSpec(ctx context.Context, svc *domain.Service, spec model.DiffSpec, hunks, asJSON, stat, nameOnly bool, stdout, stderr io.Writer) int {
	if hunks {
		files, err := svc.DiffHunks(ctx, spec)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if asJSON {
			if err := hunksJSON(stdout, files); err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
			return 0
		}
		renderHunks(stdout, files)
		return 0
	}
	if stat || nameOnly {
		stats, err := svc.DiffStat(ctx, spec)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if nameOnly {
			for _, s := range stats {
				fmt.Fprintln(stdout, s.Path)
			}
			return 0
		}
		renderStat(stdout, stats)
		return 0
	}
	patch, err := svc.DiffPatch(ctx, spec)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	io.WriteString(stdout, patch)
	return 0
}

// splitDashDash splits raw args at the first literal "--": everything
// before it goes to flag parsing (flags plus at most one rev), everything
// after is paths. The split must happen BEFORE fs.Parse — flag.Parse
// consumes a leading "--" itself, which would misread the first path as
// a rev when no rev is given.
func splitDashDash(args []string) (head, paths []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// partitionFlags splits args (already past splitDashDash, so no literal "--"
// remains) into flag-ish args ("-" prefixed) and positionals, preserving
// relative order within each group. This lets a command accept its flag
// either before or after its positional (e.g. `gg show HEAD --patch`), which
// flag.Parse alone can't do — it stops at the first non-flag argument. It is
// only safe for command flag sets made entirely of bool flags: a bool flag
// never consumes a following argument as its value, so moving flag-ish
// tokens out of position can't strand an intended flag value as a positional.
func partitionFlags(args []string) (flagArgs, posArgs []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
		} else {
			posArgs = append(posArgs, a)
		}
	}
	return flagArgs, posArgs
}

// renderStat prints the terse stat block: "path +A -D" per file ("path bin"
// for binaries, "old => new +A -D" for renames) and an "N files +A -D"
// trailer. Empty input prints nothing.
func renderStat(w io.Writer, stats []model.DiffStat) {
	if len(stats) == 0 {
		return
	}
	add, del := 0, 0
	for _, s := range stats {
		name := s.Path
		if s.OldPath != "" {
			name = s.OldPath + " => " + s.Path
		}
		if s.Binary {
			fmt.Fprintf(w, "%s bin\n", name)
			continue
		}
		add += s.Added
		del += s.Deleted
		fmt.Fprintf(w, "%s +%d -%d\n", name, s.Added, s.Deleted)
	}
	fmt.Fprintf(w, "%d files +%d -%d\n", len(stats), add, del)
}

// renderHunks prints one path line per file followed by its numbered hunks:
//
//	src/search.ts
//	  1 @@ -15,7 +15,9 @@ export function score
//	  2 @@ -40,3 +42,8 @@
//
// The @@ line is REBUILT from the parsed range, so a header git wrote with an
// omitted count ("@@ -40 +42 @@") prints with explicit counts. A file with no
// hunks (binary) still prints its path, so an agent sees it changed.
func renderHunks(w io.Writer, files []model.FileHunks) {
	for _, f := range files {
		name := f.Path
		if f.OldPath != "" {
			name = f.OldPath + " => " + f.Path
		}
		fmt.Fprintln(w, name)
		for _, h := range f.Hunks {
			line := fmt.Sprintf("  %d @@ -%s +%s @@", h.N, hunkSideSpan(h.Old), hunkSideSpan(h.New))
			if h.Header != "" {
				line += " " + h.Header
			}
			fmt.Fprintln(w, line)
		}
	}
}

// hunkSideSpan renders one side of a rebuilt @@ header as "start,count".
func hunkSideSpan(r [2]int) string {
	if r == [2]int{0, 0} {
		return "0,0"
	}
	return fmt.Sprintf("%d,%d", r[0], r[1]-r[0]+1)
}

// wireHunk / wireFileHunks are the --json shape (spec §4.5):
// [{"path":"src/search.ts","hunks":[{"n":1,"old":[15,21],"new":[15,23],"header":"…"}]}]
type wireHunk struct {
	N      int    `json:"n"`
	Old    [2]int `json:"old"`
	New    [2]int `json:"new"`
	Header string `json:"header"`
}

type wireFileHunks struct {
	Path    string     `json:"path"`
	OldPath string     `json:"old_path,omitempty"`
	Hunks   []wireHunk `json:"hunks"`
}

func hunksJSON(w io.Writer, files []model.FileHunks) error {
	out := make([]wireFileHunks, 0, len(files))
	for _, f := range files {
		wf := wireFileHunks{Path: f.Path, OldPath: f.OldPath, Hunks: []wireHunk{}}
		for _, h := range f.Hunks {
			wf.Hunks = append(wf.Hunks, wireHunk{N: h.N, Old: h.Old, New: h.New, Header: h.Header})
		}
		out = append(out, wf)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "")
	return enc.Encode(out)
}
