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
	"github.com/homeend/gigagit/internal/steer"
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
	// A gg:// link as the FIRST positional names the target — and the
	// CHECKOUT. The note is stored where the link points, and the housekeeping
	// below (including the `reload notes` post to a live session) then belongs
	// to that worktree, not the caller's.
	var link *domain.Resolved
	if len(rest) > 0 && isLinkArg(rest[0]) {
		res, err := resolveLinkArg(context.Background(), svc, rest[0])
		if err != nil {
			return linkExit("note "+sub, err, stderr)
		}
		if msg := noteLinkShape(sub, res); msg != "" {
			fmt.Fprintf(stderr, "note %s: %s\n", sub, msg)
			return 2
		}
		link, rest = &res, rest[1:]
		svc = openLinkTarget(res)
	}
	return withNotesHousekeeping(svc, sub, func() int {
		switch sub {
		case "add":
			return noteAdd(svc, link, rest, stdout, stderr)
		case "reply":
			return noteReply(svc, rest, stdout, stderr)
		case "rm":
			return noteRemove(svc, rest, stdout, stderr)
		case "list":
			return noteList(svc, link, rest, stdout, stderr)
		case "clear":
			return noteClear(svc, link, rest, stdout, stderr)
		case "apply":
			return noteApply(svc, link, rest, stdin, stdout, stderr)
		default:
			fmt.Fprintf(stderr, "note: unknown subcommand %q\n", sub)
			return 2
		}
	})
}

// noteLinkShape says whether a gg:// link fits the sub-command it leads, and
// if not, why (an empty string means it fits). A link replaces the flags that
// name the same thing; a part of it the verb cannot use is a usage error, never
// something silently ignored:
//
//   - add, list — a FILE link (the address is the point).
//   - reply, rm — a REPOSITORY link only: the note id names the note, the link
//     just picks the checkout whose store holds it (ids are per repository).
//   - clear — either: a bare repository link picks the checkout and
//     --file/--all apply as usual (a target on it is refused, nothing would
//     use it); a file link stands in for --file/--cached/--rev.
//   - apply — a repository or target link: `gg://<repo>`, `@staged` or `@<sha>`
//     stand in for --cached/--rev; each batch item names its own path, so a
//     path on the link is refused for the same reason --file is.
func noteLinkShape(sub string, res domain.Resolved) string {
	hasPath := res.Addr.Path != ""
	hasTarget := res.Addr.State != model.StateUnstaged
	switch sub {
	case "add", "list":
		if !hasPath {
			return "that link names a repository, not a file"
		}
	case "reply", "rm":
		if hasPath || hasTarget {
			return "pass the repository's link (gg://<repo> or gg:///abs/path), not a file or commit: the note id names the note"
		}
	case "clear":
		// A repository link only picks the checkout; a target on it would be
		// dropped on the floor (the flags decide what is cleared), so refuse
		// it rather than let `gg://repo@<sha> --all` look commit-scoped.
		if !hasPath && hasTarget {
			return "a repository link cannot carry @staged or @<sha> here: pass a file link (gg://<repo>/<path>[@<target>]) or the bare repository link with --file/--all"
		}
	case "apply":
		if hasPath {
			return "pass a repository or target link (gg://<repo>, gg://<repo>@staged, gg://<repo>@<sha>): each batch item names its own path"
		}
	}
	return ""
}

// noteIDExit reports a per-id failure (reply, rm). An unknown id is the one
// error an agent hits from the WRONG checkout — note ids are per repository —
// so instead of a bare "not found" it names the store that was searched and
// the two ways to reach the right one.
func noteIDExit(sub, id string, svc *domain.Service, err error, stderr io.Writer) int {
	if errors.Is(err, domain.ErrNoteNotFound) {
		where := "this repository's store"
		if top, terr := svc.TopLevel(context.Background()); terr == nil && top != "" {
			where = "the store of " + top
		}
		fmt.Fprintf(stderr, "%s: no note %s in %s (notes are per repository: run this inside the checkout that holds it, or put that repository's link first: gg %s gg://<repo> %s …)\n", sub, id, where, sub, id)
		return 1
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}

// noteMutations are the sub-commands that CHANGE the store; only they are worth
// waking a live session for. `list` reads.
var noteMutations = map[string]bool{"add": true, "reply": true, "rm": true, "clear": true, "apply": true}

// withNotesHousekeeping applies [notes] from the effective config, runs fn, and
// only THEN starts and briefly waits for the sweep — the caller's work must
// never queue behind maintenance. A successful mutation also posts a
// best-effort `reload notes` to any live gg session for this worktree, so a
// note an agent just wrote appears in the human's open window without a manual
// refresh (srcNotes is never polled).
func withNotesHousekeeping(svc *domain.Service, sub string, fn func() int) int {
	if cfg, err := svc.EffectiveConfig(context.Background()); err == nil {
		svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
	}
	code := fn()
	if code == 0 && noteMutations[sub] {
		steer.NotifyReload(steerDirFor(svc), "notes")
	}
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
		spec, err := svc.HunkDiffSpec(ctx, cached, rev, []string{addr.Path})
		if err != nil {
			return "", [2]int{}, err
		}
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

func noteAdd(svc *domain.Service, link *domain.Resolved, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	pf := addPreviewFlag(fs)
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
	// A gg:// link is only ever recognised as the FIRST argument after the
	// subcommand (cmdNote's own rule) — so a link stuck after --flags
	// (`note add --summary x gg://…`) is never picked up as one, and Go's
	// flag package silently leaves it as a trailing positional here instead
	// of erroring. Catch it explicitly, or it is dropped on the floor and
	// the command silently keeps running against the wrong target.
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "note add: unexpected argument %q; a gg:// link must be the first argument\n", fs.Arg(0))
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
	var addr model.FileAddress
	var side model.NoteSide
	var rng [2]int
	if link != nil {
		// Ruling 9: a link and a preview are two ways of naming a target, so
		// one overriding the other silently is never right — without this
		// guard `gg note add gg://… --preview X` would drop --preview and
		// write to the LINK's target.
		if pf.set() {
			return previewUsageErr("note add", stderr)
		}
		if *tf.file != "" || *tf.rev != "" || *tf.cached || *hunk != 0 || *newLine != 0 || *oldLine != 0 {
			fmt.Fprintln(stderr, "note add: a gg:// link already names the target and the anchor (drop --file, --rev, --cached, --hunk, --new-line and --old-line)")
			return 2
		}
		addr = link.Addr
		switch {
		case link.Hunk > 0:
			// linkDiffSpec is the ONE place that maps a link's target onto a
			// diff spec, so `gg note add <link>#N` cannot drift from
			// `gg diff <link> --hunks`'s numbering.
			spec, err := linkDiffSpec(ctx, svc, *link)
			if err != nil {
				return noteExit(err, stderr)
			}
			s, r, err := svc.HunkRange(ctx, spec, addr.Path, link.Hunk)
			if err != nil {
				return noteExit(err, stderr)
			}
			side, rng = s, r
		case link.Line > 0:
			side, rng = link.Side, [2]int{link.Line, link.Line}
		default:
			fmt.Fprintln(stderr, "note add: the link names a file but no anchor; add :<line> or #<hunk>")
			return 2
		}
	} else if pf.set() {
		// A merge preview: the note is an ORDINARY committed note on the source
		// tip (the preview's new side is byte for byte the file there), but its
		// hunk numbers come from the preview's own patch.
		if *tf.rev != "" || *tf.cached {
			return previewUsageErr("note add", stderr)
		}
		if strings.TrimSpace(*tf.file) == "" {
			fmt.Fprintln(stderr, "note add: --preview needs --file <path>")
			return 2
		}
		if *oldLine != 0 {
			// Spec §1.1: the preview's old side is the merge base, which no
			// stored address names.
			fmt.Fprintln(stderr, "note add: notes in a preview anchor on the new side (drop --old-line)")
			return 2
		}
		if (*hunk != 0) == (*newLine != 0) {
			fmt.Fprintln(stderr, "note add: pass exactly one of --hunk or --new-line")
			return 2
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		// The address is an ORDINARY committed one on the tip, so it is built
		// by NoteTarget like every other: that is what normalises the path
		// (`./a.txt`, a Windows `sub\a.txt`) and refuses one escaping the repo.
		a, aerr := svc.NoteTarget(ctx, *tf.file, false, tgt.Set.Tip)
		if aerr != nil {
			return noteExit(aerr, stderr)
		}
		addr = a
		if *newLine != 0 {
			if *newLine < 1 {
				fmt.Fprintln(stderr, "note add: --new-line must be a 1-based line number")
				return 2
			}
			side, rng = model.NoteSideNew, [2]int{*newLine, *newLine}
		} else {
			// Ruling 1: the PREVIEW's patch, so this agrees with
			// `gg diff --preview --hunks`. Both refusals — a hunk number that
			// is not 1-based, and a delete-only hunk — live in domain, shared
			// with MCP; only the exit-code mapping is the CLI's.
			s, r, herr := svc.PreviewHunkAnchor(ctx, tgt.Set, addr.Path, *hunk)
			if errors.Is(herr, domain.ErrPreviewOldSide) {
				fmt.Fprintln(stderr, "note add:", herr)
				return 2
			}
			// A non-1-based hunk comes back as ErrNoteTargetUsage, which
			// noteExit already reports as the usage error it is (exit 2).
			if herr != nil {
				return noteExit(herr, stderr)
			}
			side, rng = s, r
		}
	} else {
		a, err := svc.NoteTarget(ctx, *tf.file, *tf.cached, *tf.rev)
		if err != nil {
			return noteExit(err, stderr)
		}
		s, r, err := noteAnchor(ctx, svc, a, *tf.cached, *tf.rev, *hunk, *newLine, *oldLine)
		if err != nil {
			return noteExit(err, stderr)
		}
		addr, side, rng = a, s, r
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
		if isLinkArg(fs.Arg(0)) {
			fmt.Fprintf(stderr, "note reply: unexpected argument %q; a gg:// link must be the first argument\n", fs.Arg(0))
			return 2
		}
		fmt.Fprintln(stderr, "usage: gg note reply [<repo-link>] <note-id> --summary \"…\"")
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
		return noteIDExit("note reply", id, svc, err, stderr)
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
		for _, a := range fs.Args() {
			if isLinkArg(a) {
				fmt.Fprintf(stderr, "note rm: unexpected argument %q; a gg:// link must be the first argument\n", a)
				return 2
			}
		}
		fmt.Fprintln(stderr, "usage: gg note rm [<repo-link>] <note-id>")
		return 2
	}
	// A root takes its replies with it (domain.NoteRemove).
	if err := svc.NoteRemove(context.Background(), fs.Arg(0)); err != nil {
		return noteIDExit("note rm", fs.Arg(0), svc, err, stderr)
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
//
// status is the word printed for r.Status: the raw one, or the preview's
// ("outdated" for stale, spec §1.2 — the STORE is unchanged, only the word).
func renderNoteLine(w io.Writer, r domain.ResolvedNote, indent bool, status string) {
	if indent {
		fmt.Fprintf(w, "  %s [%s] reply  %s\n", r.Note.ID, r.Note.Source, r.Note.Summary)
		return
	}
	fmt.Fprintf(w, "%s [%s] %s %s:%d-%d %s  %s\n",
		r.Note.ID, r.Note.Source, noteTargetLabel(r.Note.Address),
		r.Note.Side, r.Range[0], r.Range[1], status, r.Note.Summary)
}

// noteStatusWord is renderNoteLine's status argument: the preview's word when
// the caller is rendering a merge preview, else the stored one.
func noteStatusWord(r domain.ResolvedNote, preview bool) string {
	if preview {
		return domain.PreviewStatus(r.Status)
	}
	return string(r.Status)
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

func noteList(svc *domain.Service, link *domain.Resolved, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	pf := addPreviewFlag(fs)
	typ := fs.String("type", "all", "user, agent or all")
	asJSON := fs.Bool("json", false, "emit the wire notes as a JSON array")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// See noteAdd's identical guard: a gg:// link is only recognised as the
	// FIRST argument (cmdNote's rule), so one stuck after --flags
	// (`note list --type agent gg://…`) is never picked up as a link — Go's
	// flag package would otherwise leave it as a silently-ignored trailing
	// positional, and the command would list EVERY note instead of erroring.
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "note list: unexpected argument %q; a gg:// link must be the first argument\n", fs.Arg(0))
		return 2
	}
	if !noteTypeMatches(*typ, model.NoteSourceUser) && !noteTypeMatches(*typ, model.NoteSourceAgent) {
		fmt.Fprintln(stderr, "note list: --type must be user, agent or all")
		return 2
	}
	ctx := context.Background()
	var res []domain.ResolvedNote
	previewWords := false
	if link != nil {
		// Ruling 9: a link and a preview both name a target; combining them is
		// a usage error, never a silent override (see noteAdd's guard).
		if pf.set() {
			return previewUsageErr("note list", stderr)
		}
		if *tf.file != "" || *tf.rev != "" || *tf.cached {
			fmt.Fprintln(stderr, "note list: a gg:// link already names the target (drop --file, --rev and --cached)")
			return 2
		}
		// A link's line and hunk are ignored: `note list` is about a FILE's
		// threads, exactly as `--file` is.
		got, err := svc.NotesAt(ctx, link.Addr)
		if err != nil {
			return noteExit(err, stderr)
		}
		res = got
	} else if pf.set() {
		if *tf.rev != "" || *tf.cached {
			return previewUsageErr("note list", stderr)
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		got, gerr := previewResolvedNotes(ctx, svc, tgt.Set, strings.TrimSpace(*tf.file))
		if gerr != nil {
			return noteExit(gerr, stderr)
		}
		res = got
		previewWords = true
	} else {
		got, err := resolvedNotesFor(ctx, svc, *tf.file, *tf.cached, *tf.rev)
		if err != nil {
			return noteTargetExit("note list", err, stderr)
		}
		res = got
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
			wires = append(wires, domain.ToWireNotePreview(r, previewWords))
		}
		if err := json.NewEncoder(stdout).Encode(wires); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	for _, r := range kept {
		renderNoteLine(stdout, r, false, noteStatusWord(r, previewWords))
		for _, rep := range r.Replies {
			renderNoteLine(stdout, rep, true, noteStatusWord(rep, previewWords))
		}
	}
	return 0
}

// previewResolvedNotes gathers a preview's notes for one path, or — with no
// --file — for every path it covers, in one store load (domain.PreviewNotesAll;
// PreviewNotesAt needs a path, because it resolves against that file's content
// at the tip).
func previewResolvedNotes(ctx context.Context, svc *domain.Service, set domain.PreviewNoteSet, file string) ([]domain.ResolvedNote, error) {
	if file != "" {
		return svc.PreviewNotesAt(ctx, set, file)
	}
	byPath, err := svc.PreviewNotesAll(ctx, set)
	if err != nil {
		return nil, err
	}
	var out []domain.ResolvedNote
	for _, p := range domain.PreviewNotePaths(byPath) {
		out = append(out, byPath[p]...)
	}
	return out, nil
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
func noteClear(svc *domain.Service, link *domain.Resolved, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	all := fs.Bool("all", false, "clear every note this checkout can see")
	typ := fs.String("type", "all", "user, agent or all")
	yes := fs.Bool("yes", false, "confirm the deletion (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "note clear: unexpected argument %q; a gg:// link must be the first argument\n", fs.Arg(0))
		return 2
	}
	// A FILE link stands in for --file/--cached/--rev, as in `note list`; a
	// repository link only picked the checkout (cmdNote opened it) and the
	// --file/--all rule below applies unchanged.
	fileLink := link != nil && link.Addr.Path != ""
	hasFile := strings.TrimSpace(*tf.file) != ""
	if fileLink {
		if hasFile || *tf.rev != "" || *tf.cached {
			fmt.Fprintln(stderr, "note clear: a gg:// link already names the target (drop --file, --rev and --cached)")
			return 2
		}
		if *all {
			fmt.Fprintln(stderr, "note clear: a file link and --all name different things; pass one")
			return 2
		}
	} else if hasFile == *all {
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
	var addrs []model.FileAddress
	if fileLink {
		// The link's line and hunk are ignored: `clear` is about a FILE's
		// threads, exactly as --file is.
		addrs = []model.FileAddress{link.Addr}
	} else {
		got, err := noteAddressesFor(ctx, svc, *tf.file, *tf.cached, *tf.rev)
		if err != nil {
			return noteTargetExit("note clear", err, stderr)
		}
		addrs = got
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
