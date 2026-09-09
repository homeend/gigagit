package cli

// `gg note …` — the agent lane's write surface over the phase 1 note store.
//
// Every verb is short-lived: it applies the effective [notes] policy, does its
// own work FIRST, then gives the startup housekeeping sweep a bounded budget
// (withNotesHousekeeping). A sweep that does not finish in time is abandoned;
// it is idempotent and lock-protected, so the next gg start retries it.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// notesSweepBudget is how long a one-shot `gg note …` waits for housekeeping
// after its own work is done. Short on purpose: the user's command has already
// succeeded, and the sweep is best-effort maintenance.
const notesSweepBudget = 2 * time.Second

// cmdNote dispatches `gg note <add|reply|rm|list|clear|apply> ...`.
func cmdNote(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg note <add|reply|rm|list|clear|apply> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	return withNotesHousekeeping(svc, func() int {
		switch sub {
		case "add":
			return noteAdd(svc, rest, stdout, stderr)
		case "reply":
			return noteReply(svc, rest, stdout, stderr)
		case "rm":
			return noteRemove(svc, rest, stdout, stderr)
		case "list":
			return noteList(svc, rest, stdout, stderr)
		case "clear":
			return noteClear(svc, rest, stdout, stderr)
		case "apply":
			return noteApply(svc, rest, stdin, stdout, stderr)
		default:
			fmt.Fprintf(stderr, "note: unknown subcommand %q\n", sub)
			return 2
		}
	})
}

// withNotesHousekeeping applies [notes] from the effective config, runs fn, and
// only THEN starts and briefly waits for the sweep — the caller's work must
// never queue behind maintenance.
func withNotesHousekeeping(svc *domain.Service, fn func() int) int {
	if cfg, err := svc.EffectiveConfig(context.Background()); err == nil {
		svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
	}
	code := fn()
	svc.StartNotesSweep()
	ctx, cancel := context.WithTimeout(context.Background(), notesSweepBudget)
	defer cancel()
	svc.WaitNotesSweep(ctx)
	return code
}

// noteTargetFlags is the shared --cached/--rev/--file flag trio.
type noteTargetFlags struct {
	file   *string
	cached *bool
	rev    *string
}

func addTargetFlags(fs *flag.FlagSet) noteTargetFlags {
	return noteTargetFlags{
		file:   fs.String("file", "", "repo-relative path the note anchors to"),
		cached: fs.Bool("cached", false, "anchor to the staged diff (HEAD → index)"),
		rev:    fs.String("rev", "", "anchor to a commit's own change (parent → commit)"),
	}
}

// noteAuthorDefault picks the author label: an explicit --author, else
// $GG_AGENT (the agent harness's own name), else the literal "agent". Thin
// wrapper over domain.NoteAuthorDefault, which the MCP note tools share.
func noteAuthorDefault(given string) string {
	return domain.NoteAuthorDefault(given)
}

// noteSourceValue validates --source. The CLI defaults to agent (a human
// scripting notes passes --source user); the TUI and web keep user.
func noteSourceValue(given string) (model.NoteSource, error) {
	switch strings.TrimSpace(given) {
	case "", "agent":
		return model.NoteSourceAgent, nil
	case "user":
		return model.NoteSourceUser, nil
	}
	return "", fmt.Errorf("--source must be user or agent")
}

// noteAnchor turns the three mutually exclusive anchor flags into the side and
// 1-based inclusive range a note occupies. --hunk resolves through the SAME
// patch `gg diff --hunks` numbers for this target.
func noteAnchor(ctx context.Context, svc *domain.Service, addr model.FileAddress, cached bool, rev string, hunk, newLine, oldLine int) (model.NoteSide, [2]int, error) {
	set := 0
	for _, v := range []int{hunk, newLine, oldLine} {
		if v != 0 {
			set++
		}
	}
	if set != 1 {
		return "", [2]int{}, fmt.Errorf("%w: pass exactly one of --hunk, --new-line or --old-line", domain.ErrNoteTargetUsage)
	}
	switch {
	case newLine != 0:
		if newLine < 1 {
			return "", [2]int{}, fmt.Errorf("%w: --new-line must be a 1-based line number", domain.ErrNoteTargetUsage)
		}
		return model.NoteSideNew, [2]int{newLine, newLine}, nil
	case oldLine != 0:
		if oldLine < 1 {
			return "", [2]int{}, fmt.Errorf("%w: --old-line must be a 1-based line number", domain.ErrNoteTargetUsage)
		}
		return model.NoteSideOld, [2]int{oldLine, oldLine}, nil
	default:
		if hunk < 1 {
			return "", [2]int{}, fmt.Errorf("%w: --hunk must be a 1-based hunk number", domain.ErrNoteTargetUsage)
		}
		spec := domain.HunkDiffSpec(cached, rev, []string{addr.Path})
		return svc.HunkRange(ctx, spec, addr.Path, hunk)
	}
}

// noteExit maps a domain error onto an exit code: 2 for a caller mistake in the
// target flags, 1 for everything else (an unreadable side, a missing note).
func noteExit(err error, stderr io.Writer) int {
	if errors.Is(err, domain.ErrNoteTargetUsage) {
		fmt.Fprintln(stderr, "note:", strings.TrimPrefix(err.Error(), "note target usage: "))
		return 2
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}

// noteTargetExit reports a noteAddressesFor/resolvedNotesFor error under a
// specific subcommand name ("note list", "note clear"), so the
// --cached/--rev-without---file usage message names the verb the user typed
// rather than a generic "note:" prefix. Every other error falls through to
// noteExit unchanged.
func noteTargetExit(sub string, err error, stderr io.Writer) int {
	if errors.Is(err, errCachedRevNeedFile) {
		fmt.Fprintf(stderr, "%s: --cached/--rev need --file <path>\n", sub)
		return 2
	}
	return noteExit(err, stderr)
}

// printNote prints a stored note: its id on one line, or the shared wire object
// with --json. A freshly written note is active by construction, so its wire
// status is "active" and its resolved range is the stored one.
func printNote(w io.Writer, n model.Note, asJSON bool) error {
	if !asJSON {
		_, err := fmt.Fprintln(w, n.ID)
		return err
	}
	wire := domain.ToWireNote(domain.ResolvedNote{Note: n, Status: model.NoteActive, Range: n.Range})
	return json.NewEncoder(w).Encode(wire)
}

func noteAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	hunk := fs.Int("hunk", 0, "anchor to git @@ hunk N of the file (see gg diff --hunks)")
	newLine := fs.Int("new-line", 0, "anchor to a 1-based line on the NEW side")
	oldLine := fs.Int("old-line", 0, "anchor to a 1-based line on the OLD side")
	summary := fs.String("summary", "", "the note (required)")
	rationale := fs.String("rationale", "", "the why, optional")
	author := fs.String("author", "", "author label (default: $GG_AGENT, else agent)")
	source := fs.String("source", "", "user or agent (default agent)")
	asJSON := fs.Bool("json", false, "print the created note as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*summary) == "" {
		fmt.Fprintln(stderr, "note add: --summary is required")
		return 2
	}
	src, err := noteSourceValue(*source)
	if err != nil {
		fmt.Fprintln(stderr, "note add:", err)
		return 2
	}
	ctx := context.Background()
	addr, err := svc.NoteTarget(ctx, *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		return noteExit(err, stderr)
	}
	side, rng, err := noteAnchor(ctx, svc, addr, *tf.cached, *tf.rev, *hunk, *newLine, *oldLine)
	if err != nil {
		return noteExit(err, stderr)
	}
	stored, err := svc.NoteAdd(ctx, model.Note{
		Source: src, Author: noteAuthorDefault(*author), Address: addr,
		Side: side, Range: rng,
		Summary: strings.TrimSpace(*summary), Rationale: strings.TrimSpace(*rationale),
	})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := printNote(stdout, stored, *asJSON); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func noteReply(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	// The note id is a leading positional, not a flag: peel it off before
	// fs.Parse, whose Go semantics stop consuming flags at the first
	// non-flag token and would otherwise swallow every flag that follows it.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, "usage: gg note reply <note-id> --summary \"…\"")
		return 2
	}
	id, rest := args[0], args[1:]
	fs := flag.NewFlagSet("note reply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	summary := fs.String("summary", "", "the reply (required)")
	rationale := fs.String("rationale", "", "the why, optional")
	author := fs.String("author", "", "author label (default: $GG_AGENT, else agent)")
	source := fs.String("source", "", "user or agent (default agent)")
	asJSON := fs.Bool("json", false, "print the created reply as JSON")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: gg note reply <note-id> --summary \"…\"")
		return 2
	}
	if strings.TrimSpace(*summary) == "" {
		fmt.Fprintln(stderr, "note reply: --summary is required")
		return 2
	}
	src, err := noteSourceValue(*source)
	if err != nil {
		fmt.Fprintln(stderr, "note reply:", err)
		return 2
	}
	// The reply's anchor is the parent's: domain copies address, side, range and
	// fingerprint, and flattens a reply-to-a-reply onto the thread root.
	stored, err := svc.NoteReply(context.Background(), id, model.Note{
		Source: src, Author: noteAuthorDefault(*author),
		Summary: strings.TrimSpace(*summary), Rationale: strings.TrimSpace(*rationale),
	})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := printNote(stdout, stored, *asJSON); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func noteRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg note rm <note-id>")
		return 2
	}
	// A root takes its replies with it (domain.NoteRemove).
	if err := svc.NoteRemove(context.Background(), fs.Arg(0)); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// noteTypeMatches applies --type. "all" (the default) keeps everything.
func noteTypeMatches(want string, src model.NoteSource) bool {
	switch want {
	case "", "all":
		return true
	case "user":
		return src == model.NoteSourceUser
	case "agent":
		return src == model.NoteSourceAgent
	}
	return false
}

// errCachedRevNeedFile is returned by noteAddressesFor when --cached/--rev is
// given without --file. It has no anchor to resolve against without a file,
// so silently falling back to "every address this checkout can see" (as a
// bare invocation does) would answer a different question than the one
// asked; callers turn this into a subcommand-specific usage message.
var errCachedRevNeedFile = errors.New("--cached/--rev need --file <path>")

// noteAddressesFor is the target-flag resolver shared by list and clear: one
// address when --file is given, else every address this checkout can see
// (NoteAddresses). --cached/--rev only make sense against a single --file
// target, so either without --file is rejected rather than silently ignored.
func noteAddressesFor(ctx context.Context, svc *domain.Service, file string, cached bool, rev string) ([]model.FileAddress, error) {
	if strings.TrimSpace(file) != "" {
		addr, err := svc.NoteTarget(ctx, file, cached, rev)
		if err != nil {
			return nil, err
		}
		return []model.FileAddress{addr}, nil
	}
	if cached || strings.TrimSpace(rev) != "" {
		return nil, errCachedRevNeedFile
	}
	return svc.NoteAddresses(ctx)
}

// resolvedNotesFor gathers the resolved threads a --file / bare invocation
// covers: one address when --file is given, else every address this checkout
// can see (NoteAddresses). Orphaned notes never appear — NotesAt drops them by
// contract, the same rule the TUI and web rows follow.
func resolvedNotesFor(ctx context.Context, svc *domain.Service, file string, cached bool, rev string) ([]domain.ResolvedNote, error) {
	addrs, err := noteAddressesFor(ctx, svc, file, cached, rev)
	if err != nil {
		return nil, err
	}
	var out []domain.ResolvedNote
	for _, a := range addrs {
		got, err := svc.NotesAt(ctx, a)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// renderNoteLine prints one thread row:
//
//	a1b2c3d4 [agent] src/search.ts new:15-23 active  Prefix matches now outrank …
//	  e5f6a7b8 [user] reply  Addressed in the latest revision
//
// A reply carries no anchor of its own — it inherits the root's — so its line
// says "reply" where the root names its file, side and range.
func renderNoteLine(w io.Writer, r domain.ResolvedNote, indent bool) {
	if indent {
		fmt.Fprintf(w, "  %s [%s] reply  %s\n", r.Note.ID, r.Note.Source, r.Note.Summary)
		return
	}
	fmt.Fprintf(w, "%s [%s] %s %s:%d-%d %s  %s\n",
		r.Note.ID, r.Note.Source, noteTargetLabel(r.Note.Address),
		r.Note.Side, r.Range[0], r.Range[1], r.Status, r.Note.Summary)
}

// noteTargetLabel names a note's target in one column: the path, prefixed with
// the short commit for a commit note so two notes on the same path never look
// identical.
func noteTargetLabel(a model.FileAddress) string {
	if a.State == model.StateCommitted && len(a.Commit) >= 7 {
		return a.Commit[:7] + ":" + a.Path
	}
	return a.Path
}

func noteList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	typ := fs.String("type", "all", "user, agent or all")
	asJSON := fs.Bool("json", false, "emit the wire notes as a JSON array")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !noteTypeMatches(*typ, model.NoteSourceUser) && !noteTypeMatches(*typ, model.NoteSourceAgent) {
		fmt.Fprintln(stderr, "note list: --type must be user, agent or all")
		return 2
	}
	ctx := context.Background()
	res, err := resolvedNotesFor(ctx, svc, *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		return noteTargetExit("note list", err, stderr)
	}
	kept := make([]domain.ResolvedNote, 0, len(res))
	for _, r := range res {
		if noteTypeMatches(*typ, r.Note.Source) {
			kept = append(kept, r)
		}
	}
	if *asJSON {
		wires := make([]domain.WireNote, 0, len(kept))
		for _, r := range kept {
			wires = append(wires, domain.ToWireNote(r))
		}
		if err := json.NewEncoder(stdout).Encode(wires); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	for _, r := range kept {
		renderNoteLine(stdout, r, false)
		for _, rep := range r.Replies {
			renderNoteLine(stdout, rep, true)
		}
	}
	return 0
}

// noteClear deletes every note thread at one address (--file) or every
// address this checkout can see (--all), and requires --yes either way.
//
// The printed count is what the write actually removed: RECORDS (roots and
// replies), never a pre-read — the same thing the TUI's "Removed N notes"
// notice counts. The default --type all path removes through
// domain.NotesClear, one store write per address, and sums NotesClear's own
// return values directly. A non-"all" --type narrows to matching ROOTS, which
// NotesClear cannot express (it takes a whole address indiscriminately): that
// case reads the roots via NotesAt, then removes each matching root with
// NoteRemove (which takes its replies with it), counting 1+len(replies) per
// root actually removed. Either way, an orphaned note (its anchor file gone,
// so `list` never shows it) is still cleared and counted: the --type all path
// never consults NotesAt at all, and NotesClear's sweep matches on the raw
// stored address, not on resolved/orphan status.
func noteClear(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	all := fs.Bool("all", false, "clear every note this checkout can see")
	typ := fs.String("type", "all", "user, agent or all")
	yes := fs.Bool("yes", false, "confirm the deletion (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	hasFile := strings.TrimSpace(*tf.file) != ""
	if hasFile == *all {
		fmt.Fprintln(stderr, "note clear: pass exactly one of --file <path> or --all")
		return 2
	}
	if !noteTypeMatches(*typ, model.NoteSourceUser) && !noteTypeMatches(*typ, model.NoteSourceAgent) {
		fmt.Fprintln(stderr, "note clear: --type must be user, agent or all")
		return 2
	}
	if !*yes {
		fmt.Fprintln(stderr, "note clear: refusing to delete without --yes")
		return 2
	}
	ctx := context.Background()
	addrs, err := noteAddressesFor(ctx, svc, *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		return noteTargetExit("note clear", err, stderr)
	}
	allTypes := strings.TrimSpace(*typ) == "" || *typ == "all"
	removed := 0
	for _, addr := range addrs {
		if allTypes {
			dropped, err := svc.NotesClear(ctx, addr)
			removed += dropped
			if err != nil {
				fmt.Fprintf(stdout, "removed %d notes\n", removed)
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
			continue
		}
		res, err := svc.NotesAt(ctx, addr)
		if err != nil {
			fmt.Fprintf(stdout, "removed %d notes\n", removed)
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		for _, r := range res {
			if !noteTypeMatches(*typ, r.Note.Source) {
				continue
			}
			if err := svc.NoteRemove(ctx, r.Note.ID); err != nil {
				fmt.Fprintf(stdout, "removed %d notes\n", removed)
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
			removed += 1 + len(r.Replies)
		}
	}
	fmt.Fprintf(stdout, "removed %d notes\n", removed)
	return 0
}
