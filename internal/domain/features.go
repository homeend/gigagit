package domain

import "github.com/homeend/gigagit/internal/preflight"

// Feature and store IDs are English protocol values: they appear in CLI
// output, MCP errors and config, and are never translated.
const (
	FeatureCore     = "core"
	FeatureVersions = "versions"
	FeatureForge    = "forge"

	StoreVersions = "versions"
)

// VersionsFormat is the branch-version layout this build writes. Format 1 was
// "a ref pointing at the pre-operation tip" — the layout gg has always used.
// Format 2 adds the Ours/Other/Base/Source/Target preview fields recorded at
// snapshot time (see snapshotBranchTipNamed); format-1 refs never carry a
// base and so can never be converted, only discarded (see the Migrate
// declaration below).
const VersionsFormat = 2

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
				Describe: func() preflight.Text {
					return preflight.Text{
						Format: "Discards every branch version recorded before this build. They cannot be converted: the old format records no merge base, so they can never open as a preview. Until you migrate, NO new versions are recorded — rebases and merges run without a safety net.",
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
