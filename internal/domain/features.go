package domain

import "github.com/homeend/gigagit/internal/preflight"

// Feature and store IDs are English protocol values: they appear in CLI
// output, MCP errors and config, and are never translated.
const (
	FeatureCore     = "core"
	FeatureVersions = "versions"
	FeaturePreviews = "previews"
	FeatureForge    = "forge"

	StoreVersions = "versions"
	StorePreviews = "previews"
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
			ID:          FeatureForge,
			Criticality: preflight.Optional,
			Silent:      true, // no forge CLI is the normal case, not a problem to report
			Requires:    []preflight.Requirement{preflight.ForgeUsable{}},
		},
	}
}
