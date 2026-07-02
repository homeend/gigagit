package model

// RepoHealth is the cheap repo-health snapshot behind the notification
// center: stat-level filesystem facts plus one config lookup — no expensive
// git walks, safe to run in the background on every repo load.
type RepoHealth struct {
	GitCommonDir          string // absolute git common dir (doubles as the per-repo dismissal key)
	PackBytes             int64  // total size of *.pack under objects/pack
	HasCommitGraph        bool   // objects/info/commit-graph file OR commit-graphs/ chain dir present
	WriteCommitGraphSet   bool   // fetch.writeCommitGraph set in local or global scope
	WriteCommitGraphValue string // the set value ("" when unset)
}
