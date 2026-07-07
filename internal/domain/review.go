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
// to feed the agent. Working changes use the zero DiffSpec (Rev "", Cached false).
type ReviewTarget struct {
	Kind  ReviewKind
	Range string // "" for the working-changes target
	Diff  model.DiffSpec
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
	if strings.TrimSpace(res.Captured) == "" {
		return ReviewResult{}, fmt.Errorf("review produced an empty report")
	}
	path, werr := s.writeReviewReport(ctx, target.Range, res.Captured, now)
	if werr != nil {
		return ReviewResult{}, werr
	}
	return ReviewResult{Path: path, Content: res.Captured, Range: target.Range}, nil
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
func (s *Service) BranchReviewTarget(ctx context.Context, tip string) (ReviewTarget, error) {
	base, err := s.repo.MergeBase(ctx, "main", tip)
	if err != nil || strings.TrimSpace(base) == "" {
		if up, uerr := s.repo.UpstreamRef(ctx, tip); uerr == nil && strings.TrimSpace(up) != "" {
			base = strings.TrimSpace(up)
		} else {
			// no base found: review just the tip commit's own change (vs its parent)
			rng := tip + "^.." + tip
			return ReviewTarget{Kind: ReviewBranch, Range: rng, Diff: model.DiffSpec{Rev: rng}}, nil
		}
	}
	base = strings.TrimSpace(base)
	rng := base + ".." + tip
	return ReviewTarget{Kind: ReviewBranch, Range: rng, Diff: model.DiffSpec{Rev: rng}}, nil
}
