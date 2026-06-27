package observ

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// FailureEntry is one genuine, frontend-surfaced failure: a git operation or
// read query that returned an error to a frontend.
type FailureEntry struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	Detail string    `json:"detail"`
}

// failureRingCap bounds the in-memory session ring read by SessionFailures.
const failureRingCap = 500

var (
	failMu   sync.Mutex
	failRing []FailureEntry
	failSink io.Writer
)

// SetFailureSink registers (nil clears) the durable writer that every
// subsequent NoteFailure appends one line to. Ring collection is independent
// and always on. Mirrors SetSpanSink: nil (the default) means ring-only, so
// the CLI/library/tests see no file side effect unless they opt in.
func SetFailureSink(w io.Writer) {
	failMu.Lock()
	defer failMu.Unlock()
	failSink = w
}

// NoteFailure records a genuine failure. A nil error or a context
// cancellation/deadline (a user abort, not a git failure) is ignored. The
// entry joins a bounded process-global ring and, when a sink is set, is
// appended there as one tab-separated line.
func NoteFailure(source string, err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	e := FailureEntry{Time: time.Now(), Source: source, Detail: oneLine(err.Error())}
	failMu.Lock()
	defer failMu.Unlock()
	failRing = append(failRing, e)
	if len(failRing) > failureRingCap {
		failRing = failRing[len(failRing)-failureRingCap:]
	}
	if failSink != nil {
		fmt.Fprintf(failSink, "%s\t%s\t%s\n", e.Time.UTC().Format(time.RFC3339), e.Source, e.Detail)
	}
}

// SessionFailures returns a newest-first copy of the failure ring.
func SessionFailures() []FailureEntry {
	failMu.Lock()
	defer failMu.Unlock()
	out := make([]FailureEntry, len(failRing))
	for i, e := range failRing {
		out[len(failRing)-1-i] = e
	}
	return out
}

// ResetFailures clears the ring and sink. Tests use it for isolation.
func ResetFailures() {
	failMu.Lock()
	defer failMu.Unlock()
	failRing = nil
	failSink = nil
}

// oneLine collapses all runs of whitespace (including newlines) to single
// spaces so a multi-line git stderr renders as one log line / one list row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
