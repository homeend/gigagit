package model

import "time"

// PR states are English protocol values (CLI --json, future MCP); the TUI
// localizes only their rendering.
const (
	PRStateOpen        = "open"
	PRStateClosed      = "closed"
	PRStateMerged      = "merged"
	PRStateUnavailable = "unavailable" // known locally, the forge no longer answers for it
)

// PullRequest is one forge pull/merge request, provider-neutral.
type PullRequest struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body,omitempty"`
	Author      string    `json:"author"`
	State       string    `json:"state"`
	Draft       bool      `json:"draft"`
	ReviewState string    `json:"review_state,omitempty"` // "", approved, changes_requested, review_required
	Source      string    `json:"source"`                 // head branch name
	SourceRepo  string    `json:"source_repo,omitempty"`  // owner/name when the head lives in a fork
	Target      string    `json:"target"`                 // base branch name
	HeadSHA     string    `json:"head_sha"`
	BaseSHA     string    `json:"base_sha"`
	URL         string    `json:"url"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

// IsOpen reports whether p is still open on the forge.
func (p PullRequest) IsOpen() bool { return p.State == PRStateOpen }

// ForgeCommentKind classifies a comment by where it hangs.
type ForgeCommentKind string

const (
	ForgeCommentInline  ForgeCommentKind = "inline"  // anchored to a line (range)
	ForgeCommentFile    ForgeCommentKind = "file"    // anchored to a whole file
	ForgeCommentGeneral ForgeCommentKind = "general" // the PR conversation
	ForgeCommentReview  ForgeCommentKind = "review"  // a submitted review's summary + verdict
)

// ForgeComment is one comment of any kind. IDs are the forge's, opaque.
type ForgeComment struct {
	ID        string           `json:"id"`
	ParentID  string           `json:"parent_id,omitempty"`
	Kind      ForgeCommentKind `json:"kind"`
	Author    string           `json:"author"`
	Body      string           `json:"body"`
	Path      string           `json:"path,omitempty"`
	Side      NoteSide         `json:"side,omitempty"`
	Line      int              `json:"line,omitempty"`
	StartLine int              `json:"start_line,omitempty"` // == Line for a one-line comment
	Outdated  bool             `json:"outdated,omitempty"`
	Resolved  bool             `json:"resolved,omitempty"`
	Hunk      string           `json:"hunk,omitempty"`
	Verdict   string           `json:"verdict,omitempty"` // review only: approved, changes_requested, commented
	Created   time.Time        `json:"created"`
	Updated   time.Time        `json:"updated"`
}
