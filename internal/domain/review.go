package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// ReviewKind names the three review targets.
type ReviewKind int

const (
	ReviewBranch ReviewKind = iota
	ReviewRange
	ReviewWorking
)

// ReviewTarget is a resolved review scope: the DiffSpec to feed the agent, the
// injection-safe Range that the command's <range> token / DiffSpec.Rev use, and
// a human-friendly Label for every DISPLAY surface (status bar, viewer title,
// report filename, the agent's # Range: context header).
//
// Range vs Label is a security boundary, not cosmetics: Range must be pure hex
// (or a user-typed rev) because it is spliced UNQUOTED into `claude -p
// "/code-review <range>"` — a branch name there is a command-injection vector
// (git allows $()/backticks in ref names). Label is NEVER executed; it is only
// rendered or written to a file, so it may carry a branch name or commit
// subject freely. See BranchReviewTarget's comment.
//
// Working changes use Diff.Rev "HEAD" (git diff HEAD — the full working-tree +
// staged diff), NOT the zero DiffSpec, which is bare `git diff` and would
// silently omit staged changes. See WorkingReviewTarget.
type ReviewTarget struct {
	Kind  ReviewKind
	Range string // injection-safe: hex SHA range / user-typed rev. "" for working changes.
	Label string // human display: branch name / "<short> <subject>" / typed range / "working changes"
	Diff  model.DiffSpec
	// Commit is the full sha a review NOTE anchors to: the range's last
	// commit (a branch review's tip). Branch is set only when the target was
	// named by a branch ref. Both are data, never spliced into a command.
	Commit string
	Branch string
	// Preview is the scope a review belongs to when it reviewed a merge
	// preview ("<target>...<source>", branch NAMES) or a commit pair
	// ("<a7>..<b7>"): Note.Preview. Such a review is its preview's, never
	// its commit's. Data only — never spliced into a command.
	Preview string
	// Focus is what the user asked the reviewer to look at hardest; it
	// travels in the review brief, never in a command. "" = none.
	Focus string
}

// DisplayLabel is the human string shown for this target (status bar, viewer
// title, report filename). It falls back Label → Range → "working changes": a
// construction site that forgets Label degrades to the (visible) old hex-range
// behavior rather than silently mislabeling every report "working changes".
func (t ReviewTarget) DisplayLabel() string {
	if s := strings.TrimSpace(t.Label); s != "" {
		return s
	}
	if s := strings.TrimSpace(t.Range); s != "" {
		return s
	}
	// The TUI (reviewScopeLabel, reviewTitle) matches this literal byte-for-byte
	// to route it through a translated key — domain must not import i18n, so
	// rewording it here silently degrades that surface to untranslated English.
	return "working changes"
}

// WorkingReviewTarget is the "review my uncommitted changes" target: the full
// working-tree + staged diff vs HEAD (git diff HEAD), NOT bare `git diff`
// which would omit staged changes (git diff = working tree vs index only).
// The single source of truth for both the CLI (`gg review --working`) and the
// TUI (Files panel "Review working changes") so the two call sites can't
// diverge. Its Label "working changes" is matched byte-for-byte by the TUI's
// reviewTitle to pick the translated sibling key ("Review: working changes")
// instead of the generic "Review: %s" format — see DisplayLabel's fallback.
func WorkingReviewTarget() ReviewTarget {
	return ReviewTarget{Kind: ReviewWorking, Range: "", Label: "working changes", Diff: model.DiffSpec{Rev: "HEAD"}}
}

// ScopeReviewTarget is the review of a merge preview or a commit pair: Range
// = the scope's hex pair (merge base..source tip, or a..b), Label = the human
// pair, Commit = the tip, Preview = the scope's name. A pull request's set
// has no portable scope name, so its review is stored untagged (as before).
// The ONE constructor: `gg review --preview`, `gg review save`, the TUI and
// the web all build a scope review here.
func ScopeReviewTarget(set PreviewNoteSet) ReviewTarget {
	spec := set.DiffSpec()
	label := set.Target + " ... " + set.Source
	if set.IsPair() {
		label = shortSHA(set.Base) + ".." + shortSHA(set.Tip)
	}
	return ReviewTarget{Kind: ReviewRange, Range: spec.Rev, Label: label, Diff: spec, Commit: set.Tip, Preview: set.scope()}
}

// ReviewResult is a produced review: its text, the note it was stored as (a
// working-changes review is stored in this worktree's notes), the
// injection-safe range, and the human Label used for titles.
type ReviewResult struct {
	NoteID  string
	Warn    string // the note store had to be moved aside first
	Content string
	Range   string
	Label   string
	// Structured: the reply was the review document (Content is then its
	// canonical form); false = prose, kept as text.
	Structured bool
}

// ReviewReport runs resolvedCommand over target via engine.ReviewChanges and
// stores the captured review as a note on the reviewed commit (SaveReview).
// The three-frontend entry point; agent names the tool (the note's author). A
// working-changes review is stored in this worktree's notes, with the
// fingerprints of the files it read (spec 2026-10-04 working reviews).
// When the store cannot keep it, the result still carries the report
// (Content, Label, Structured; no NoteID) beside a "review not saved" error.
func (s *Service) ReviewReport(ctx context.Context, target ReviewTarget, agent, resolvedCommand string, env []string) (ReviewResult, error) {
	out, files, err := s.runReview(ctx, target, resolvedCommand, env)
	if err != nil {
		return ReviewResult{}, err
	}
	id, warn, serr := s.SaveReview(ctx, SaveReview{Target: target, Agent: agent, Text: out.Content, Files: files})
	if serr != nil {
		// The report is the agent's work: hand it back beside the error so
		// the caller can still show it.
		return out, fmt.Errorf("review not saved: %w", serr)
	}
	out.NoteID, out.Warn = id, warn
	return out, nil
}

// RunReview is ReviewReport without the store: it runs resolvedCommand over
// target and returns the parsed review (NoteID stays ""). A cross-review's
// reviewers run this way; only the merged review is stored.
func (s *Service) RunReview(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string) (ReviewResult, error) {
	out, _, err := s.runReview(ctx, target, resolvedCommand, env)
	return out, err
}

// runReview runs the review op and parses its capture: the result and, for a
// working review, the reviewed files' fingerprints SaveReview records.
func (s *Service) runReview(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string) (ReviewResult, []model.NoteFile, error) {
	label := target.DisplayLabel()
	op := engine.ReviewChanges{
		Command:    resolvedCommand,
		Dir:        s.workdir,
		Env:        env,
		Diff:       target.Diff,
		RangeLabel: label, // the agent's "# Range:" context header — display text, not executed
		Working:    target.Kind == ReviewWorking,
		Focus:      target.Focus,
	}
	res, err := s.Execute(ctx, op, nil, nil)
	if err != nil {
		return ReviewResult{}, nil, err
	}
	// Claude's --output-format json wraps the markdown report in a JSON
	// envelope ({"result":"<markdown>",...}); unwrap it here so the stored
	// note and the returned Content are the markdown report, not the raw
	// JSON blob. Junie's raw-text $GG_MESSAGE_FILE path (and any plain-text
	// tool) passes through unchanged.
	report, perr := exttool.ParseCaptureReport(res.Captured)
	if perr != nil {
		return ReviewResult{}, nil, perr
	}
	report = strings.TrimSpace(report)
	if report == "" {
		return ReviewResult{}, nil, fmt.Errorf("review produced an empty report")
	}
	report = canonicalReview(report)
	_, perr = notebatch.ParseReview([]byte(report))
	return ReviewResult{Content: report, Range: target.Range, Label: label, Structured: perr == nil}, res.ReviewFiles, nil
}

// BranchReviewTarget resolves <base>..<tip>: base = merge-base with the trunk
// (trunkFor: origin's default branch, else main, else master), then
// @{upstream}, else the tip alone (a branch with no base -> review just its
// tip). The trunk wins over the upstream: a branch tracking its own pushed
// copy is still reviewed in full, not just its unpushed commits.
//
// Both endpoints are resolved to their full commit SHA before being
// substituted into Range (which an external-tool command later splices as
// unquoted prose into `claude -p "/code-review <range>"`). A raw ref name
// here would be an injection vector: git allows `$(...)`/backtick command
// substitutions in ref names, and those execute inside the command's double
// quotes. tip is the obvious case (the TUI passes a user-created branch
// name); base needs the same treatment because the @{upstream} fallback
// yields a ref name too (e.g. "origin/feature", from a hostile remote branch
// auto-tracked by a local branch whose merge-base with the trunk doesn't exist)
// — not a SHA, so it's just as injectable if left unresolved. Resolving both
// closes it off: Range/Diff.Rev are pure hex, never carry a ref name. The
// one visible tradeoff: a branch review's report title and filename now show
// a SHA range instead of the branch name.
func (s *Service) BranchReviewTarget(ctx context.Context, tip string) (ReviewTarget, error) {
	tipSHA, err := s.repo.ResolveCommit(ctx, tip)
	if err != nil {
		return ReviewTarget{}, err
	}
	branch := s.reviewBranchName(ctx, tip, tipSHA)
	// The trunk is not skipped for itself here: reviewing the trunk measures
	// it against itself (an empty range), as it always has.
	var base string
	if trunk, terr := s.trunkFor(ctx, ""); terr != nil {
		return ReviewTarget{}, terr
	} else if trunk != "" {
		base, err = s.repo.MergeBase(ctx, trunk, tipSHA)
	}
	if err != nil || strings.TrimSpace(base) == "" {
		if up, uerr := s.repo.UpstreamRef(ctx, tip); uerr == nil && strings.TrimSpace(up) != "" {
			base = strings.TrimSpace(up)
		} else {
			// no base found: review just the tip commit's own change (vs its parent)
			rng := tipSHA + "^.." + tipSHA
			return ReviewTarget{Kind: ReviewBranch, Range: rng, Label: tip, Diff: model.DiffSpec{Rev: rng}, Commit: tipSHA, Branch: branch}, nil
		}
	}
	baseSHA, err := s.repo.ResolveCommit(ctx, strings.TrimSpace(base))
	if err != nil {
		return ReviewTarget{}, err
	}
	rng := baseSHA + ".." + tipSHA
	// Range is the hex range (executed); Label is the branch NAME (display only).
	return ReviewTarget{Kind: ReviewBranch, Range: rng, Label: tip, Diff: model.DiffSpec{Rev: rng}, Commit: tipSHA, Branch: branch}, nil
}
