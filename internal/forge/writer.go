package forge

import (
	"context"
	"regexp"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// Event is a submitted review's verdict.
type Event string

const (
	EventComment        Event = "COMMENT"
	EventApprove        Event = "APPROVE"
	EventRequestChanges Event = "REQUEST_CHANGES"
)

// Thread is one review thread to create. Line 0 makes it file-level.
type Thread struct {
	Path      string
	Line      int
	StartLine int
	Side      model.NoteSide
	Body      string
}

// ThreadRef is a created thread: its id, its first comment's id and URL.
type ThreadRef struct{ ID, CommentID, URL string }

// CommentRef is a created reply.
type CommentRef struct{ ID, URL string }

// Writer posts to a forge. It is optional beside Provider (which stays
// read-only); domain reaches it only through engine.SendToForge. v2's PR
// lifecycle and metadata methods join this interface.
type Writer interface {
	// StartReview opens the viewer's pending (unsubmitted) review on commit.
	StartReview(ctx context.Context, prID, commit string) (reviewID string, err error)
	AddThread(ctx context.Context, reviewID string, t Thread) (ThreadRef, error)
	// Reply answers a thread; reviewID "" posts it on its own (submitted at
	// once), otherwise it joins that pending review.
	Reply(ctx context.Context, reviewID, threadID, body string) (CommentRef, error)
	SubmitReview(ctx context.Context, reviewID string, ev Event, body string) error
	DeletePendingReview(ctx context.Context, reviewID string) error
	Resolve(ctx context.Context, threadID string) error
	Unresolve(ctx context.Context, threadID string) error
}

const markerOpen, markerClose = "<!-- gg:", " -->"

// markerRe matches a gg send marker on a line of its own.
var markerRe = regexp.MustCompile(`(?m)^<!-- gg:([A-Za-z0-9:._-]+) -->[ \t]*$`)

// SendMarker is the invisible last line of every body gg posts: the local
// item's key, so an echo is matched to its local copy even when the send's
// stamp was lost.
func SendMarker(key string) string { return markerOpen + key + markerClose }

// SendMarkerKey is the key of body's LAST marker.
func SendMarkerKey(body string) (string, bool) {
	all := markerRe.FindAllStringSubmatch(body, -1)
	if len(all) == 0 {
		return "", false
	}
	return all[len(all)-1][1], true
}

// StripSendMarker drops every marker line and the blank lines it leaves at
// the end.
func StripSendMarker(body string) string {
	if !strings.Contains(body, markerOpen) {
		return body
	}
	return strings.TrimRight(markerRe.ReplaceAllString(body, ""), " \t\r\n")
}

// IsBlankBodyError reports the forge refusing a review because its body is
// empty (the REST rule; whether GraphQL applies it to COMMENT is unverified).
func IsBlankBodyError(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "body") && (strings.Contains(m, "blank") || strings.Contains(m, "empty"))
}
