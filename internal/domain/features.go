package domain

import "github.com/homeend/gigagit/internal/preflight"

// Feature and store IDs are English protocol values: they appear in CLI
// output, MCP errors and config, and are never translated.
const (
	FeatureCore     = "core"
	FeatureVersions = "versions"
	FeaturePreviews = "previews"
	FeatureForge    = "forge"

	FeatureStructuredReviews = "structured-reviews"

	FeatureNotes = "notes"
	StoreNotes   = "notes"

	StoreVersions       = "versions"
	StorePreviews       = "previews"
	StoreReviewCommands = "review-commands"
)

// VersionsFormat is the branch-version layout this build writes. Format 1 was
// "a ref pointing at the pre-operation tip" — the layout gg has always used.
// Format 2 adds the Ours/Other/Base/Source/Target preview fields recorded at
// snapshot time (see snapshotBranchTipNamed); format-1 refs never carry a
// base and so can never be converted, only discarded (see the Migrate
// declaration below).
const VersionsFormat = 2

// PreviewsFormat is the saved-comparison layout this build writes. Format 1
// was the standalone previews.toml merge-preview store; format 2 is the
// savedcompare store that absorbs it (spec §4.5). Unlike VersionsFormat this
// number is NOT a DataFormat range: previews.toml and savedcompare.toml are
// different FILES, so the requirement asks presence (preflight.LegacyStore)
// and the number only labels the marker once the conversion has run.
const PreviewsFormat = 2

// NotesFormat is the note-store layout this build writes. Format 1 was the
// single notes.toml; format 2 splits it into one file per part (spec
// 2026-10-04). Like PreviewsFormat it only labels the marker: the
// requirement asks presence of the legacy file (preflight.LegacyStore).
const NotesFormat = 2

// MinGitVersion is the oldest git gg supports. 2.30 ships the for-each-ref and
// worktree behaviour every frontend assumes.
var MinGitVersion = [3]int{2, 30, 0}

// Features is the v1 registry. Adding a feature here is how it gains a
// requirement contract; nothing else has to change.
func Features() []preflight.Feature {
	return []preflight.Feature{
		{
			ID:          FeatureCore,
			Criticality: preflight.Required,
			Requires:    []preflight.Requirement{preflight.GitVersion{Min: MinGitVersion}},
		},
		{
			ID:          FeatureVersions,
			Criticality: preflight.Optional,
			Requires: []preflight.Requirement{
				preflight.DataFormat{Store: StoreVersions, Min: VersionsFormat, Max: VersionsFormat},
			},
			// Format-1 refs cannot be converted (no merge base to recover),
			// so the migration is a straight discard: ApplyMigration deletes
			// the listed refs and stamps the format-2 marker.
			Migrate: &preflight.Migration{
				Store: StoreVersions, From: 1, To: 2,
				Action: "discard-refs",
				Describe: func() preflight.Text {
					return preflight.Text{
						Format: "Discards every branch version recorded before this build. They cannot be converted: the old format records no merge base, so they can never open as a preview. Until you migrate, NO new versions are recorded — rebases and merges run without a safety net.",
					}
				},
			},
		},
		{
			ID:          FeaturePreviews,
			Criticality: preflight.Optional,
			Requires: []preflight.Requirement{
				preflight.LegacyStore{Store: StorePreviews},
			},
			// LOSSLESS, so this runs without asking: every saved preview is
			// converted into the savedcompare store with its id, label and
			// creation time intact, and preview notes need no migration at
			// all — they key on the branch NAMES. There is nothing to
			// confess, so there is nothing to ask.
			Migrate: &preflight.Migration{
				Store: StorePreviews, From: 1, To: PreviewsFormat,
				Action: "convert-previews", Lossless: true,
				Describe: func() preflight.Text {
					return preflight.Text{
						Format: "Folds your saved merge previews into the saved-comparison store. Nothing is lost: ids, labels and creation times are kept, and preview notes are unaffected.",
					}
				},
			},
		},
		{
			ID:          FeatureNotes,
			Criticality: preflight.Optional,
			// An older gg (an installed gg mcp) may recreate notes.toml
			// mid-session; the next start merges it. Never worth a notice.
			Silent: true,
			Requires: []preflight.Requirement{
				preflight.LegacyStore{Store: StoreNotes},
			},
			// LOSSLESS: every note's address names its file, the merge is by
			// id with the newer copy winning, and the old file is kept as a
			// backup — there is nothing to confess, so nothing to ask.
			Migrate: &preflight.Migration{
				Store: StoreNotes, From: 1, To: NotesFormat,
				Action: "split-notes", Lossless: true,
				Describe: func() preflight.Text {
					return preflight.Text{
						Format: "Splits the note store into one file per worktree plus files for commits, previews and shelf entries. Nothing is lost: every note keeps its id, and the old file is kept as a backup.",
					}
				},
			},
		},
		{
			ID:          FeatureStructuredReviews,
			Criticality: preflight.Optional,
			Requires: []preflight.Requirement{
				preflight.LegacyStore{Store: StoreReviewCommands},
			},
			// First-run detection wrote the built-in review commands INTO the
			// user's config, so a machine that ran an older gg still asks its
			// agents for a free-form report. Rewriting the user's config asks
			// first: NOT lossless. Only a command byte-identical to an old
			// built-in rendering is touched (exttool.UpgradeReviewCommand).
			Migrate: &preflight.Migration{
				Store: StoreReviewCommands, From: 1, To: 2,
				Action: "upgrade-review-commands",
				Describe: func() preflight.Text {
					return preflight.Text{
						Format: "Updates the review commands gg once wrote into your config to the structured-review prompt, so reviews open as a rendered review view. Only commands identical to an old built-in are changed; commands you edited are left alone.",
					}
				},
			},
		},
		{
			ID:          FeatureForge,
			Criticality: preflight.Optional,
			Silent:      true, // no forge CLI is the normal case, not a problem to report
			Requires:    []preflight.Requirement{preflight.ForgeUsable{}},
		},
	}
}
