package domain

import "github.com/homeend/gigagit/internal/preflight"

// Feature and store IDs are English protocol values: they appear in CLI
// output, MCP errors and config, and are never translated.
const (
	FeatureCore     = "core"
	FeatureVersions = "versions"

	StoreVersions = "versions"
)

// VersionsFormat is the branch-version layout this build writes. Format 1 is
// "a ref pointing at the pre-operation tip" — the layout gg has always used.
const VersionsFormat = 1

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
			// No Migrate: format 2 and its migration arrive in the follow-up
			// spec. Repairable has only test consumers until then.
		},
	}
}
