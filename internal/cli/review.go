package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
// the captured report to stdout, and persists it via domain.ReviewReport.
// Exit 0 on a produced report, 1 on tool failure/empty report/no review tool
// configured, 2 on a flag/usage error.
//
// Flags must come BEFORE the positional (like `gg log [-n N] [<rev>]`, unlike
// `gg show <commit> [--patch]`): --tool takes a value, and flag.Parse stops
// at the first non-flag argument, so a value-taking flag can't safely be
// partitioned out from after a positional the way show's bool-only --patch
// is (see partitionFlags's doc comment in diff.go).
func cmdReview(svc *domain.Service, workdir string, rest []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	toolName := fs.String("tool", "", "review tool name (from config); default: the only one")
	working := fs.Bool("working", false, "review uncommitted working changes")
	wantNotes := fs.Bool("notes", false, "also ask the tool for anchored notes (agent-context v1) and import them")
	pf := addPreviewFlag(fs)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if *working && fs.NArg() >= 1 {
		fmt.Fprintln(stderr, "usage: gg review [--tool <name>] [--working] [<rev>|<A..B>]")
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: gg review [--tool <name>] [--working] [<rev>|<A..B>]")
		return 2
	}
	ctx := context.Background()

	// Resolve the target.
	arg := ""
	var target domain.ReviewTarget
	// hunkSpec is the patch a `hunk` annotation is numbered against; only
	// --preview has one of its own (nil = derive it from the target as before).
	var hunkSpec *model.DiffSpec
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
		target = domain.ReviewTarget{Kind: domain.ReviewRange, Range: tgt.Spec.Rev,
			Label: scopeName(tgt.Set), Diff: tgt.Spec}
		spec := tgt.Spec
		hunkSpec = &spec
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

	notesPath := ""
	if *wantNotes {
		// The CLI owns this file: the op only names it in the environment, and
		// we must still be able to read it once the op returns.
		f, terr := os.CreateTemp("", "gg-review-notes-*.json")
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		notesPath = f.Name()
		f.Close()
		defer os.Remove(notesPath)
		// A note write must respect the configured entry cap even though this
		// process is not a `gg note` verb.
		if cfg, cerr := loadConfigFor(svc); cerr == nil {
			svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
		}
	}

	res, err := svc.ReviewReportNotes(ctx, target, resolved, []string{"GG_TASK=review"}, time.Now(), notesPath)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	io.WriteString(stdout, res.Content)
	if !strings.HasSuffix(res.Content, "\n") {
		io.WriteString(stdout, "\n")
	}
	fmt.Fprintln(stderr, "report:", res.Path)
	if !*wantNotes {
		return 0
	}
	return importReviewNotes(ctx, svc, target, arg, notesPath, res.Content, cmd.Name, hunkSpec, stderr)
}

// reviewImportTarget decides which diff a review's notes anchor to (§4.5):
//
//	gg review <sha>        → that commit; BOTH sides are addressable
//	gg review A..B / branch→ the TIP commit; NEW side only
//	gg review --working    → the unstaged working tree; NEW side only
//
// The two new-side-only cases exist because a review's base (the merge base, or
// HEAD for --working) is not one of §4.4's note-addressable old sides.
func reviewImportTarget(ctx context.Context, svc *domain.Service, target domain.ReviewTarget, arg string) (cached bool, rev string, rule domain.NoteSideRule, err error) {
	if target.Kind == domain.ReviewWorking {
		return false, "", domain.NoteSideNewOnly, nil
	}
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

// importReviewNotes reads the tool's notes: the sidecar file when it is
// non-empty, else the captured report when THAT parses as agent-context v1
// (some tools have only one output channel). Neither → exit 1.
//
// hunkSpec is the patch a `hunk` annotation is numbered against, or nil to
// derive it from cached/rev as before. Only --preview passes one: its notes are
// stored on the source tip but numbered over merge-base → tip, and nothing else
// in review land splits those two apart. It is threaded EXPLICITLY rather than
// sniffed from the range string, which would silently renumber today's
// `gg review A...B --notes`.
func importReviewNotes(ctx context.Context, svc *domain.Service, target domain.ReviewTarget, arg, notesPath, report, toolName string, hunkSpec *model.DiffSpec, stderr io.Writer) int {
	data, _ := os.ReadFile(notesPath)
	if len(strings.TrimSpace(string(data))) == 0 {
		if s := strings.TrimSpace(report); strings.HasPrefix(s, "{") {
			data = []byte(s)
		}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		fmt.Fprintln(stderr, "error: review tool wrote no notes (expected agent-context v1 at $GG_NOTES_FILE)")
		return 1
	}
	batch, err := notebatch.Parse(data)
	if err != nil {
		fmt.Fprintln(stderr, "error: review tool wrote no notes (expected agent-context v1 at $GG_NOTES_FILE):", err)
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
		domain.NoteBatchTarget{Cached: cached, Rev: rev, Hunks: hunkSpec},
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
