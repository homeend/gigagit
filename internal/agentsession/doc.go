// Package agentsession runs interactive programs (AI agents, shells) in
// pseudo-terminals with an in-memory terminal emulator, so a frontend can
// paint a live console and forward keystrokes. A Manager owns every session
// of the process; nothing here knows about git, repos or the TUI — callers
// pass the working directory and the repo NAME used for grouping.
//
// Each Session runs three goroutines: PTY → emulator (the child's output),
// emulator → PTY (query replies such as a cursor-position report, plus keys
// and pastes encoded by the emulator), and a waiter that records the exit.
// Neither pump ever waits on a consumer: Changed is a capacity-1 coalesced
// signal and taps drop when full.
package agentsession
