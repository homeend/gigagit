package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// --preview is the ONE way an agent addresses a merge preview from the CLI:
// a saved record's id or label, or git's three-dot form <target>...<source>
// (which needs no saved record). One parser, shared by diff, note and review,
// so the three can never disagree about what a preview's diff is.

// previewTarget is a resolved --preview argument.
type previewTarget struct {
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
		"a merge preview: <id>, <label>, or <target>...<source>")}
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
	source, target, err := svc.PreviewResolve(ctx, spec)
	if err != nil {
		return previewTarget{}, err
	}
	sum, err := svc.PreviewSummary(ctx, source, target)
	if err != nil {
		return previewTarget{}, err
	}
	switch sum.State {
	case domain.PreviewOK:
	case domain.PreviewMissingSource:
		return previewTarget{}, fmt.Errorf("preview: missing: %s", source)
	case domain.PreviewMissingTarget:
		return previewTarget{}, fmt.Errorf("preview: missing: %s", target)
	default:
		return previewTarget{}, fmt.Errorf("preview: %s → %s: %s", source, target, sum.State)
	}
	set, err := svc.PreviewNotes(ctx, source, target)
	if err != nil {
		return previewTarget{}, err
	}
	return previewTarget{Source: source, Target: target, Set: set, Spec: set.DiffSpec()}, nil
}

// withPaths copies the spec with -- <paths> applied.
func (t previewTarget) withPaths(paths []string) model.DiffSpec {
	s := t.Spec
	s.Paths = paths
	return s
}
