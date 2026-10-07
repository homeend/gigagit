// Package forge is gg's window onto a code forge's pull requests: Provider
// reads (and never mutates); the optional Writer posts, reached only through
// engine.SendToForge. Everything above them is forge-neutral; gh*.go are the
// only files that know GitHub. Owned by domain — frontends never import it.
package forge

import (
	"context"
	"errors"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// CallTimeout bounds every provider subprocess.
const CallTimeout = 30 * time.Second

// ErrNotFound: the forge has no such pull request (deleted, or never existed).
var ErrNotFound = errors.New("forge: pull request not found")

// Provider reads one forge. Implementations never mutate the forge.
type Provider interface {
	Name() string
	// Detect returns nil only when the tool is installed, authenticated and
	// able to read THIS repository's pull requests.
	Detect(ctx context.Context) error
	ListOpen(ctx context.Context) ([]model.PullRequest, error)
	// Search lists the pull requests matching q (already Normalized), newest
	// updated first; more reports that the forge had rows beyond q.Limit.
	Search(ctx context.Context, q PRQuery) (prs []model.PullRequest, more bool, err error)
	PR(ctx context.Context, n int) (model.PullRequest, error)
	Comments(ctx context.Context, n int) (cs []model.ForgeComment, truncated bool, err error)
	// BaseRepo names the repository PRs target: its RepoSlug and a clone URL
	// to fall back on when no configured remote matches.
	BaseRepo(ctx context.Context) (slug, fallbackURL string, err error)
	// HeadRefspec is the server-side ref holding PR n's head.
	HeadRefspec(n int) string
}

// Snapshot is one pull request and all its comments, as one read returns
// them.
type Snapshot struct {
	PR        model.PullRequest
	Comments  []model.ForgeComment
	Truncated bool
}

// Snapshotter is optional: a Provider that implements it answers a
// revalidation (the PR and its comments) with ONE call instead of two.
// Read-only, like Provider.
type Snapshotter interface {
	Snapshot(ctx context.Context, n int) (Snapshot, error)
}

// Default is the provider list, in probe order.
func Default(workDir string, rec observ.Recorder) []Provider {
	return []Provider{NewGH(workDir, rec)}
}
