package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// slotMsg is a message addressed to the worktree slot it was asked from. A
// window-addressed result (a diff, a file list, a popup's read) is stamped
// with the viewed slot when its command is built; Update drops one whose
// slot is not on screen — whether that slot sleeps or is gone — before any
// handler runs. Two slots can hold the same kind of window at the same
// generation (a versions popup in A and one in B), so the stamp, not the
// window type, says which one the answer belongs to. The handlers stay
// unaware: a message for the slot on screen is dispatched as before; one
// for a sleeping slot waits on that slot (routeSlotMsg, slot_replay.go)
// and is applied when the worktree returns; one for a gone slot is
// dropped. A message a handler writes only through the window's own
// pointer (a history walk's chunks) is deliberately NOT stamped: its view
// waits in the slot and keeps filling.
type slotMsg interface{ slotKey() model.CheckoutKey }

// sharedWriter is a slot-addressed message that ALSO carries process-wide
// state (a shelf list is the repository's, shown by the Shelf tab): the
// shared part is applied when the message is queued for a sleeping slot,
// and the message is marked so its replay does not write a by-then older
// value over a newer one.
type sharedWriter interface {
	applyShared(m Model) (Model, tea.Msg)
}

// slotStamp is the embeddable stamp; the zero value ("") is unaddressed
// and passes the gate (tests that build messages by hand, and messages
// that are not window-addressed).
type slotStamp struct{ slot model.CheckoutKey }

func (s slotStamp) slotKey() model.CheckoutKey { return s.slot }

// stamp is the slot a command built now is asked from.
func (m Model) stamp() slotStamp { return slotStamp{slot: m.viewed} }
