package web

// conflictPayload is the paused-op object /api/status carries whenever a
// sequencer op (merge/rebase/cherry-pick/revert) is in progress — including
// paused with every conflict resolved (conflicted == 0), which is what lets
// the client's Continue light up. Absent entirely when nothing is paused.
type conflictPayload struct {
	Op         string `json:"op"`
	Source     string `json:"source,omitempty"`
	Target     string `json:"target,omitempty"`
	Desc       string `json:"desc,omitempty"` // domain's human phrase ("merging feature into main")
	Conflicted int    `json:"conflicted"`
}
