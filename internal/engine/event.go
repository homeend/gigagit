// Package engine defines the frontend-agnostic operation contract: operations
// emit Events and resolve forks via a Decider, so a TUI, CLI, or MCP agent can
// all drive the same operation.
package engine

import "github.com/homeend/gigagit/internal/observ"

// Event is a tagged union streamed by an operation to its consumer.
type Event interface{ isEvent() }

// Progress reports a high-level step ("stashing", "fetching", "pulling").
type Progress struct {
	Step   string // stable step vocabulary; a TUI translates it directly as its own key
	Detail string
	// DetailMsg is the localizable channel for Detail, set (via Progressf)
	// only when Detail mixes English words into the data. Empty = Detail is
	// pure data, rendered verbatim.
	DetailMsg Msg
}

// GitLine carries one raw line of git stdout/stderr for a live log view.
type GitLine struct{ Raw string }

// DecisionNeeded is emitted alongside a Decider call so passive observers can
// render the prompt.
type DecisionNeeded struct{ Request DecisionRequest }

// Timing carries a completed span (a git subprocess or an operation step).
type Timing struct{ Span observ.Span }

// Done is the terminal event carrying the operation result.
type Done struct{ Result Result }

func (Progress) isEvent()       {}
func (GitLine) isEvent()        {}
func (DecisionNeeded) isEvent() {}
func (Timing) isEvent()         {}
func (Done) isEvent()           {}
