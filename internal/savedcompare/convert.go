package savedcompare

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

// LegacyFile is the name of the merge-preview store savedcompare absorbs.
const LegacyFile = "previews.toml"

// legacyPreview is the on-disk shape of one internal/preview record, FROZEN.
// A migration reads the bytes that are actually on a user's disk, so it keeps
// its own copy rather than importing the live type: internal/preview is
// deleted by this plan, and a later change to model.MergePreview must not be
// able to alter what this reads.
type legacyPreview struct {
	ID      string    `toml:"id"`
	Source  string    `toml:"source"`
	Target  string    `toml:"target"`
	Label   string    `toml:"label"`
	Created time.Time `toml:"created"`
}

type legacyIndex struct {
	Previews []legacyPreview `toml:"previews"`
}

// linkText renders the set link for one legacy record.
//
// TARGET FIRST. PreviewAdd(source, target) renders
// merge-base(target, source)..source, spelled `@target...source` — the same
// order `git diff target...source` reads. Swapping these two halves compares
// the wrong direction and is invisible in any fixture whose two names are
// interchangeable (spec §5.1).
func (p legacyPreview) linkText(repo model.LinkRepo) string {
	return model.Link{Repo: repo, Target: model.LinkTarget{
		State:   model.StateCommitted,
		Preview: &model.LinkPreview{Source: p.Source, Target: p.Target},
	}}.String()
}

// ConvertLegacy folds dir/previews.toml into dir/savedcompare.toml and
// removes the legacy file, returning how many records it converted.
//
// Lossless by construction: the id is carried across VERBATIM (it is a
// user-visible handle typed into `gg preview <id>`), as are the label and the
// creation time. Preview notes need no migration at all — they key on the
// branch NAMES and are ordinary committed notes at the source tip.
//
// Idempotent: a record already present is skipped (Add dedups on the pair),
// so an older gg that recreates previews.toml is absorbed rather than
// duplicated. A corrupt legacy file is an error and the file is LEFT IN
// PLACE — data is never removed on the strength of a parse this build got
// wrong.
func ConvertLegacy(dir string, repo model.LinkRepo) (int, error) {
	path := filepath.Join(dir, LegacyFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var idx legacyIndex
	if err := toml.Unmarshal(data, &idx); err != nil {
		return 0, fmt.Errorf("savedcompare: %s is corrupt: %w", path, err)
	}
	st := NewFileStore(dir)
	n := 0
	for _, p := range idx.Previews {
		if p.Source == "" || p.Target == "" {
			continue // a half-written legacy row names no pair; drop it
		}
		l, err := model.ParseLink(p.linkText(repo))
		if err != nil {
			return n, fmt.Errorf("savedcompare: converting preview %q: %w", p.ID, err)
		}
		e := Entry{ID: p.ID, Left: l, Label: p.Label, Created: p.Created}
		if _, err := st.Add(e); err != nil && !errors.Is(err, ErrExists) {
			return n, err
		} else if err == nil {
			n++
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return n, err
	}
	return n, nil
}
