package domain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
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

// ReviewResult is a produced review: the durable report path, its content, the
// injection-safe range, and the human Label used for the title/filename.
type ReviewResult struct {
	Path    string
	Content string
	Range   string
	Label   string
}

// ReviewReport runs resolvedCommand over target via engine.ReviewChanges, then
// persists the captured report under <state>/gg/reviews/<repoKey>/. now is
// injected so the filename timestamp is testable.
func (s *Service) ReviewReport(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string, now time.Time) (ReviewResult, error) {
	label := target.DisplayLabel()
	op := engine.ReviewChanges{
		Command:    resolvedCommand,
		Dir:        s.workdir,
		Env:        env,
		Diff:       target.Diff,
		RangeLabel: label, // the agent's "# Range:" context header — display text, not executed
	}
	res, err := s.Execute(ctx, op, nil, nil)
	if err != nil {
		return ReviewResult{}, err
	}
	// Claude's --output-format json wraps the markdown report in a JSON
	// envelope ({"result":"<markdown>",...}); unwrap it here so both the
	// persisted file and the returned Content are the markdown report, not
	// the raw JSON blob. Junie's raw-text $GG_MESSAGE_FILE path (and any
	// plain-text tool) passes through unchanged.
	report, perr := exttool.ParseCaptureReport(res.Captured)
	if perr != nil {
		return ReviewResult{}, perr
	}
	if strings.TrimSpace(report) == "" {
		return ReviewResult{}, fmt.Errorf("review produced an empty report")
	}
	path, werr := s.writeReviewReport(ctx, label, report, now)
	if werr != nil {
		return ReviewResult{}, werr
	}
	return ReviewResult{Path: path, Content: report, Range: target.Range, Label: label}, nil
}

// writeReviewReport persists a report under a date-foldered, human-readable
// path: <state>/gg/reviews/<repoKey>/<YYYY-MM-DD>/<HH-MM>-<label>.md, where
// label is the target's DisplayLabel (branch name / "<short> <subject>" / range
// / "working changes"), truncated and filename-sanitized. Grouping by day keeps
// the archive browsable; the label in the name says what each report is at a
// glance.
func (s *Service) writeReviewReport(ctx context.Context, label, content string, now time.Time) (string, error) {
	base := reviewsBaseDir()
	if base == "" {
		return "", fmt.Errorf("review: no state dir available")
	}
	common, err := s.GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, repoKey(strings.TrimSpace(common)), now.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := now.Format("15-04") + "-" + sanitizeRangeForFilename(truncateLabel(label, 60)) + ".md"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// truncateLabel bounds a display label to n runes (subjects are unbounded) so a
// report filename stays a reasonable length. Rune-safe; trims trailing space.
func truncateLabel(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n]), " ")
}

// reviewsBaseDir mirrors shelfBaseDir (shelfstore.go) with a "reviews" leaf.
func reviewsBaseDir() string {
	// An explicitly-set $XDG_STATE_HOME wins on every platform (it is a
	// deliberate override — and the only way tests can isolate state on
	// Windows); %LocalAppData% is the ambient Windows default.
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "reviews")
	}
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "reviews")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "reviews")
}

// sanitizeRangeForFilename replaces bytes unsafe inside one filename segment
// (/, whitespace, control bytes, ':') with '-'; '..' is kept. "" -> a stable label.
func sanitizeRangeForFilename(rng string) string {
	rng = strings.TrimSpace(rng)
	if rng == "" {
		return "working-changes"
	}
	var b strings.Builder
	for _, r := range rng {
		switch {
		case r == '/' || r == ':' || r <= ' ':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// BranchReviewTarget resolves <base>..<tip>: base = merge-base with main, then
// @{upstream}, else the tip alone (a branch with no base -> review just its tip).
//
// Both endpoints are resolved to their full commit SHA before being
// substituted into Range (which an external-tool command later splices as
// unquoted prose into `claude -p "/code-review <range>"`). A raw ref name
// here would be an injection vector: git allows `$(...)`/backtick command
// substitutions in ref names, and those execute inside the command's double
// quotes. tip is the obvious case (the TUI passes a user-created branch
// name); base needs the same treatment because the @{upstream} fallback
// yields a ref name too (e.g. "origin/feature", from a hostile remote branch
// auto-tracked by a local branch whose merge-base with main doesn't exist)
// — not a SHA, so it's just as injectable if left unresolved. Resolving both
// closes it off: Range/Diff.Rev are pure hex, never carry a ref name. The
// one visible tradeoff: a branch review's report title and filename now show
// a SHA range instead of the branch name.
func (s *Service) BranchReviewTarget(ctx context.Context, tip string) (ReviewTarget, error) {
	tipSHA, err := s.repo.ResolveCommit(ctx, tip)
	if err != nil {
		return ReviewTarget{}, err
	}
	base, err := s.repo.MergeBase(ctx, "main", tip)
	if err != nil || strings.TrimSpace(base) == "" {
		if up, uerr := s.repo.UpstreamRef(ctx, tip); uerr == nil && strings.TrimSpace(up) != "" {
			base = strings.TrimSpace(up)
		} else {
			// no base found: review just the tip commit's own change (vs its parent)
			rng := tipSHA + "^.." + tipSHA
			return ReviewTarget{Kind: ReviewBranch, Range: rng, Label: tip, Diff: model.DiffSpec{Rev: rng}}, nil
		}
	}
	baseSHA, err := s.repo.ResolveCommit(ctx, strings.TrimSpace(base))
	if err != nil {
		return ReviewTarget{}, err
	}
	rng := baseSHA + ".." + tipSHA
	// Range is the hex range (executed); Label is the branch NAME (display only).
	return ReviewTarget{Kind: ReviewBranch, Range: rng, Label: tip, Diff: model.DiffSpec{Rev: rng}}, nil
}
