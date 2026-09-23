package agentsession

import "time"

// ID names a session within its Manager ("s1", "s2", …).
type ID string

// State is a session's lifecycle state.
type State int

const (
	// Running: the child process is alive.
	Running State = iota
	// Exited: the child exited (or was killed); its last screen stays readable.
	Exited
)

// Info is a copied snapshot of a session's metadata.
type Info struct {
	ID       ID
	Label    string    // menu label, e.g. "Claude" / "Claude (yolo)"
	AgentID  string    // exttool tool id ("claude"), "" for a custom command
	Repo     string    // repository NAME for grouping (caller-computed)
	Dir      string    // worktree path = the child's cwd
	Started  time.Time // when Start returned
	State    State
	ExitCode int // valid when State == Exited; -1 = killed by a signal / unknown
}

// StartSpec describes the program to run.
type StartSpec struct {
	Label, AgentID, Repo, Dir string
	Argv                      []string // argv[0] is resolved via exec.LookPath inside Start
	Env                       []string // extra KEY=VALUE, appended after os.Environ()
	Cols, Rows                int
}

// ScrollbackLines caps the emulator's scrollback per session.
const ScrollbackLines = 10000
