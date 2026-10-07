package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/template"
)

// cmdReview implements `gg review [--tool <name>] [--working] [<rev>|<A..B>]`:
// runs the configured review agent headless over the resolved target, prints
// the captured report to stdout, and persists it via domain.ReviewReport — a
// --working review is stored as a note in this worktree's notes.
// Exit 0 on a produced report, 1 on tool failure/empty report/no review tool
// configured, 2 on a flag/usage error.
//
// Flags must come BEFORE the positional (like `gg log [-n N] [<rev>]`, unlike
// `gg show <commit> [--patch]`): --tool takes a value, and flag.Parse stops
// at the first non-flag argument, so a value-taking flag can't safely be
// partitioned out from after a positional the way show's bool-only --patch
// is (see partitionFlags's doc comment in diff.go).
func cmdReview(svc *domain.Service, workdir string, rest []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// `show` is a subcommand: it reads a stored review back. A branch named
	// show is reviewed by its full ref (gg review refs/heads/show).
	if len(rest) > 0 && rest[0] == "show" {
		return reviewShow(svc, rest[1:], stdout, stderr)
	}
	if len(rest) > 0 && rest[0] == "save" {
		return reviewSave(svc, rest[1:], stdin, stdout, stderr)
	}
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	toolName := fs.String("tool", "", "review tool name (from config); default: the only one")
	working := fs.Bool("working", false, "review uncommitted working changes")
	wantNotes := fs.Bool("notes", false, "also ask the tool for anchored notes (agent-context v1) and import them (not with --working)")
	pf := addPreviewFlag(fs)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if *working && fs.NArg() >= 1 {
		fmt.Fprintln(stderr, "usage: gg review [--tool <name>] [--working] [<rev>|<A..B>]\n       "+strings.TrimPrefix(reviewShowUsage, "usage: ")+"\n       "+strings.TrimPrefix(reviewSaveUsage, "usage: "))
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: gg review [--tool <name>] [--working] [<rev>|<A..B>]\n       "+strings.TrimPrefix(reviewShowUsage, "usage: ")+"\n       "+strings.TrimPrefix(reviewSaveUsage, "usage: "))
		return 2
	}
	if *working && *wantNotes {
		fmt.Fprintln(stderr, "gg review: --notes does not apply to --working: a working review is stored; its notes show on the files")
		return 2
	}
	ctx := context.Background()

	// Resolve the target.
	arg := ""
	var target domain.ReviewTarget
	// hunkSpec is the patch a `hunk` annotation is numbered against; only
	// --preview has one of its own (nil = derive it from the target as before).
	var hunkSpec *model.DiffSpec
	var preview string // the scope the imported notes record (a --preview review)
	switch {
	case pf.set():
		if *working || fs.NArg() >= 1 {
			return previewUsageErr("review", stderr)
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		// Ruling 9: the range review, over the pair the preview names. Range is
		// the HASH pair (it is spliced unquoted into the tool command); Label is
		// the human pair and is never executed. The existing range rule in
		// reviewImportTarget then anchors notes on the tip, new side only —
		// exactly what a preview needs.
		arg = tgt.Spec.Rev
		target = domain.ScopeReviewTarget(tgt.Set) // the preview's own review (spec R5)
		spec := tgt.Spec
		hunkSpec, preview = &spec, tgt.Set.Pair()
	case *working:
		target = domain.WorkingReviewTarget()
	case fs.NArg() >= 1:
		arg = fs.Arg(0)
		target = reviewTargetForArg(arg)
	default:
		t, err := svc.BranchReviewTarget(ctx, "HEAD")
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		target = t
	}

	// Pick the review tool command from config.
	cmd, err := selectReviewCommand(svc, *toolName, stderr)
	if err != nil {
		return 1
	}
	resolved, err := template.ResolveCommand(cmd.Command, nil, template.CmdCtx{Range: target.Range, Repo: workdir})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	if *wantNotes {
		// A note write must respect the configured entry cap even though this
		// process is not a `gg note` verb.
		if cfg, cerr := loadConfigFor(svc); cerr == nil {
			svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
		}
	}

	res, err := svc.ReviewReport(ctx, target, cmd.Name, resolved, []string{"GG_TASK=review"})
	if err != nil {
		// A report the store could not keep is still printed: the agent's
		// work is not lost because the note was not written.
		if res.Content != "" {
			printReview(stdout, res.Content)
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	printReview(stdout, res.Content)
	if res.Warn != "" {
		fmt.Fprintln(stderr, "warning:", res.Warn)
	}
	if !res.Structured {
		if res.NoteID != "" {
			fmt.Fprintln(stderr, "warning: the review is not in gg review format; stored as text")
		} else {
			fmt.Fprintln(stderr, "warning: the review is not in gg review format")
		}
	}
	if res.NoteID != "" {
		fmt.Fprintln(stderr, "note:", res.NoteID)
	}
	if !*wantNotes {
		return 0
	}
	return importReviewNotes(ctx, svc, target, arg, res.Content, cmd.Name, hunkSpec, preview, stderr)
}

// reviewImportTarget decides which diff a review's notes anchor to (§4.5):
//
//	gg review <sha>        → that commit; BOTH sides are addressable
//	gg review A..B / branch→ the TIP commit; NEW side only
//
// (A --working review is stored and draws its own notes: --notes refuses it.)
// The new-side-only case exists because a review's base (the merge base) is
// not one of §4.4's note-addressable old sides.
func reviewImportTarget(ctx context.Context, svc *domain.Service, target domain.ReviewTarget, arg string) (cached bool, rev string, rule domain.NoteSideRule, err error) {
	if arg != "" && !strings.Contains(arg, "..") {
		return false, arg, domain.NoteSideBoth, nil // a single commit: parent → commit
	}
	// A range: anchor to its TIP. "A..B" and "A...B" both end at the last
	// non-empty segment.
	parts := strings.Split(target.Range, "..")
	tip := ""
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			tip = s
		}
	}
	if tip == "" {
		return false, "", domain.NoteSideNewOnly, fmt.Errorf("cannot find the tip commit of %q", target.Range)
	}
	full, rerr := svc.RevParse(ctx, tip)
	if rerr != nil {
		return false, "", domain.NoteSideNewOnly, fmt.Errorf("unknown revision %q: %w", tip, rerr)
	}
	return false, strings.TrimSpace(full), domain.NoteSideNewOnly, nil
}

// importReviewNotes stores the review document's notes as ordinary notes: the
// report must be the structured review document (agent-context v1), else
// exit 1.
//
// hunkSpec is the patch a `hunk` annotation is numbered against, or nil to
// derive it from cached/rev as before. Only --preview passes one: its notes are
// stored on the source tip but numbered over merge-base → tip, and nothing else
// in review land splits those two apart. It is threaded EXPLICITLY rather than
// sniffed from the range string, which would silently renumber today's
// `gg review A...B --notes`.
func importReviewNotes(ctx context.Context, svc *domain.Service, target domain.ReviewTarget, arg, report, toolName string, hunkSpec *model.DiffSpec, preview string, stderr io.Writer) int {
	doc, err := notebatch.ParseReview([]byte(report))
	if err != nil {
		fmt.Fprintln(stderr, "error: the review is not a gg review document, so it has no notes to import:", err)
		return 1
	}
	if n, _ := doc.NoteCount(); n == 0 {
		return 0 // an overview only: nothing to import
	}
	batch, err := notebatch.Parse(doc.Canonical())
	if err != nil {
		fmt.Fprintln(stderr, "error: the review document's notes cannot be imported:", err)
		return 1
	}
	for _, c := range batch.Contexts {
		fmt.Fprintln(stderr, "context:", c)
	}
	cached, rev, rule, err := reviewImportTarget(ctx, svc, target, arg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	planned, skipped, err := svc.PlanNoteBatchIn(ctx, batch,
		domain.NoteBatchTarget{Cached: cached, Rev: rev, Hunks: hunkSpec, Preview: preview},
		noteAuthorDefault(toolName), rule)
	if err != nil {
		return noteExit(err, stderr)
	}
	// hunkSpec != nil is exactly the --preview case, which has its own reason.
	warnSkippedOldSide(stderr, skipped, hunkSpec != nil)
	stored, err := svc.ApplyNoteBatch(ctx, planned)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	// Only wake the window when there is something new to show: an import that
	// stored nothing (a batch of contexts only, or one whose annotations were
	// all skipped as old-side) would otherwise leave a stray wire command.
	if len(stored) > 0 {
		steer.NotifyReload(steerDirFor(svc), "notes")
	}
	ids := make([]string, 0, len(stored))
	for _, n := range stored {
		ids = append(ids, n.ID)
	}
	fmt.Fprintln(stderr, "notes:", strings.Join(ids, " "))
	return 0
}

// reviewTargetForArg classifies a single positional argument as either an
// explicit range (contains "..", used as-is) or a single commit (reviewed
// against its own parent via "<arg>^..<arg>" — model.DiffSpec{Rev: arg} alone
// would diff the WORKING TREE against arg, which is empty on a clean
// checkout, not the commit's own change).
func reviewTargetForArg(arg string) domain.ReviewTarget {
	// Label = the arg as typed (already human-readable, e.g. "main..HEAD" or a
	// short sha) for the report title/filename; Range stays the executed rev.
	if strings.Contains(arg, "..") {
		return domain.ReviewTarget{Kind: domain.ReviewRange, Range: arg, Label: arg, Diff: model.DiffSpec{Rev: arg}}
	}
	rng := arg + "^.." + arg
	return domain.ReviewTarget{Kind: domain.ReviewRange, Range: rng, Label: arg, Diff: model.DiffSpec{Rev: rng}}
}

// selectReviewCommand loads the effective config and returns the chosen
// review-category command: --tool picks by name; exactly one candidate with
// no --tool uses it; zero or more-than-one-without---tool is an error
// (listing names in the ambiguous case).
func selectReviewCommand(svc *domain.Service, name string, stderr io.Writer) (config.ToolCommand, error) {
	cfg, err := loadConfigFor(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error: loading config:", err)
		return config.ToolCommand{}, err
	}
	var cands []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(exttool.CatReview) {
			continue
		}
		if !config.ToolVisibleIn(tc, "cli") {
			continue
		}
		if tc.Mode == string(exttool.ModeInteractive) {
			continue // waits for a human; gg review runs headless
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, tc.PerFile) != nil {
			continue
		}
		cands = append(cands, tc)
	}
	if len(cands) == 0 {
		fmt.Fprintln(stderr, "error: no review tool configured (see [[tools.command]] category=\"review\")")
		return config.ToolCommand{}, fmt.Errorf("no review tool")
	}
	if name != "" {
		for _, tc := range cands {
			if tc.Name == name {
				return tc, nil
			}
		}
		fmt.Fprintf(stderr, "error: no review tool named %q\n", name)
		return config.ToolCommand{}, fmt.Errorf("no such tool")
	}
	if len(cands) > 1 {
		var names []string
		for _, tc := range cands {
			names = append(names, tc.Name)
		}
		fmt.Fprintf(stderr, "error: multiple review tools; pass --tool (%s)\n", strings.Join(names, ", "))
		return config.ToolCommand{}, fmt.Errorf("ambiguous tool")
	}
	return cands[0], nil
}

// loadConfigFor loads the effective config (global + active repo) for svc's
// repo. The resolution itself lives in domain (Service.EffectiveConfig) so the
// MCP frontend, which cannot import internal/cli, shares it.
func loadConfigFor(svc *domain.Service) (config.Config, error) {
	return svc.EffectiveConfig(context.Background())
}

// printReview writes a review for a terminal or a pipe: a review document as
// its overview, its meta, then one "path:line — summary" line per note (an
// old-side line is "-line", as in a diff); prose as it came.
func printReview(w io.Writer, content string) {
	doc, err := notebatch.ParseReview([]byte(content))
	if err != nil {
		io.WriteString(w, content)
		if !strings.HasSuffix(content, "\n") {
			io.WriteString(w, "\n")
		}
		return
	}
	fmt.Fprintln(w, strings.TrimRight(doc.Overview, "\n"))
	if len(doc.Meta) > 0 {
		fmt.Fprintf(w, "\n%s\n", metaText(doc.Meta))
	}
	first := true
	for _, f := range doc.Files {
		for _, n := range f.Notes {
			if first {
				fmt.Fprintln(w)
				first = false
			}
			line := fmt.Sprint(n.Range[0])
			if n.Range[1] != n.Range[0] {
				line += fmt.Sprintf("-%d", n.Range[1])
			}
			if n.Side == "old" {
				line = "-" + line
			}
			fmt.Fprintf(w, "%s:%s — %s", f.Path, line, n.Summary)
			if len(n.Meta) > 0 {
				fmt.Fprintf(w, " (%s)", metaText(n.Meta))
			}
			fmt.Fprintln(w)
		}
	}
}

func metaText(meta []notebatch.MetaKV) string {
	parts := make([]string, len(meta))
	for i, kv := range meta {
		parts[i] = kv.Key + ": " + kv.Value
	}
	return strings.Join(parts, ", ")
}
