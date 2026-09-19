package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// --preview is the ONE way an agent addresses a merge preview OR a commit pair
// from the CLI: a saved entry's id or label, git's three-dot form
// <target>...<source>, or the two-dot <a>..<b> (neither needs a saved entry).
// One parser — domain.NoteScopeResolve, shared with MCP — behind diff, note
// and review, so they can never disagree about what the scope's diff is.

// previewTarget is a resolved --preview argument.
type previewTarget struct {
	// Source and Target are the branch pair's NAMES; both are empty for a
	// commit pair (Set.IsPair()) — prose goes through scopeName.
	Source, Target string
	Set            domain.PreviewNoteSet
	// Spec is the PREVIEW's diff, straight from Set.DiffSpec() — the single
	// construction of that patch (ruling 1). Hunk numbers come from it, never
	// from the tip commit's own parent→tip diff, so `gg diff --preview
	// --hunks` and `gg note add --preview --hunk N` address the same hunk.
	// Hashes, not names: the value is spliced into a git argv and rides the
	// diff cache key.
	Spec model.DiffSpec
}

type previewFlag struct{ spec *string }

func addPreviewFlag(fs *flag.FlagSet) previewFlag {
	return previewFlag{spec: fs.String("preview", "",
		"a merge preview or a commit pair: <id>, <label>, <target>...<source> or <a>..<b>")}
}

func (pf previewFlag) set() bool { return pf.spec != nil && *pf.spec != "" }

// previewUsageErr is the refusal when --preview is combined with another way
// of naming a target. It is a usage error (exit 2), never a silent override.
func previewUsageErr(verb string, stderr io.Writer) int {
	fmt.Fprintf(stderr, "%s: one target only (--preview cannot be combined with --rev, --cached or a gg:// link)\n", verb)
	return 2
}

// resolvePreviewTarget resolves the argument and the pair's live state. A pair
// that is not previewable is an ERROR here (unlike the TUI/web display path,
// where it is simply nothing to show): a CLI caller asked for that diff by
// name and must not be handed an empty one.
func resolvePreviewTarget(ctx context.Context, svc *domain.Service, spec string) (previewTarget, error) {
	set, err := svc.NoteScopeResolve(ctx, spec)
	if err != nil {
		return previewTarget{}, err
	}
	return previewTarget{Source: set.Source, Target: set.Target, Set: set, Spec: set.DiffSpec()}, nil
}

// scopeName is how prose names a note scope: the branch pair, or <a7>..<b7>.
func scopeName(set domain.PreviewNoteSet) string {
	if set.IsPair() {
		return set.Base[:7] + ".." + set.Tip[:7]
	}
	return set.Target + " ... " + set.Source
}

// withPaths copies the spec with -- <paths> applied.
func (t previewTarget) withPaths(paths []string) model.DiffSpec {
	s := t.Spec
	s.Paths = paths
	return s
}

// previewTargetFromLink adapts a RESOLVED preview link onto the very value
// --preview produces, so every consumer below runs ONE code path (ruling 5).
// The set was already resolved by domain.ResolveLink on the link's own
// checkout; re-resolving here would cost two more rev-parse calls and could
// disagree with the address the resolver already handed out.
func previewTargetFromLink(res domain.Resolved) (previewTarget, bool) {
	if res.Preview == nil {
		return previewTarget{}, false
	}
	set := *res.Preview
	return previewTarget{Source: set.Source, Target: set.Target, Set: set, Spec: set.DiffSpec()}, true
}
