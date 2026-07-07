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

// ReviewTarget is a resolved review scope: a human Range label plus the DiffSpec
// to feed the agent. Working changes use Diff.Rev "HEAD" (git diff HEAD — the
// full working-tree + staged diff), NOT the zero DiffSpec, which is bare
// `git diff` and would silently omit staged changes. See WorkingReviewTarget.
type ReviewTarget struct {
	Kind  ReviewKind
	Range string // "" for the working-changes target
	Diff  model.DiffSpec
}

// WorkingReviewTarget is the "review my uncommitted changes" target: the full
// working-tree + staged diff vs HEAD (git diff HEAD), NOT bare `git diff`
// which would omit staged changes (git diff = working tree vs index only).
// The single source of truth for both the CLI (`gg review --working`) and the
// TUI (Files panel "Review working changes") so the two call sites can't
// diverge.
func WorkingReviewTarget() ReviewTarget {
	return ReviewTarget{Kind: ReviewWorking, Range: "", Diff: model.DiffSpec{Rev: "HEAD"}}
}

// ReviewResult is a produced review: the durable report path and its content.
type ReviewResult struct {
	Path    string
	Content string
	Range   string
}

// ReviewReport runs resolvedCommand over target via engine.ReviewChanges, then
// persists the captured report under <state>/gg/reviews/<repoKey>/. now is
// injected so the filename timestamp is testable.
func (s *Service) ReviewReport(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string, now time.Time) (ReviewResult, error) {
	op := engine.ReviewChanges{
		Command:    resolvedCommand,
		Dir:        s.workdir,
		Env:        env,
		Diff:       target.Diff,
		RangeLabel: target.Range,
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
	path, werr := s.writeReviewReport(ctx, target.Range, report, now)
	if werr != nil {
		return ReviewResult{}, werr
	}
	return ReviewResult{Path: path, Content: report, Range: target.Range}, nil
}

func (s *Service) writeReviewReport(ctx context.Context, rng, content string, now time.Time) (string, error) {
	base := reviewsBaseDir()
	if base == "" {
		return "", fmt.Errorf("review: no state dir available")
	}
	common, err := s.GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, repoKey(strings.TrimSpace(common)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := now.Format("20060102-1504") + "-" + sanitizeRangeForFilename(rng) + ".md"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// reviewsBaseDir mirrors shelfBaseDir (shelfstore.go) with a "reviews" leaf.
func reviewsBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "reviews")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "reviews")
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
			return ReviewTarget{Kind: ReviewBranch, Range: rng, Diff: model.DiffSpec{Rev: rng}}, nil
		}
	}
	baseSHA, err := s.repo.ResolveCommit(ctx, strings.TrimSpace(base))
	if err != nil {
		return ReviewTarget{}, err
	}
	rng := baseSHA + ".." + tipSHA
	return ReviewTarget{Kind: ReviewBranch, Range: rng, Diff: model.DiffSpec{Rev: rng}}, nil
}
