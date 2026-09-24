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
	// Cwd is where the process runs when that differs from Dir — a worktree
	// git recorded under the other environment's notation, reached through
	// its translated path. Dir stays the identity frontends group by. "" = Dir.
	Cwd  string
	Argv []string // argv[0] is resolved via exec.LookPath inside Start
	Env  []string // extra KEY=VALUE, appended after os.Environ()
	// CmdLine, when set, is the raw Windows command line handed to the
	// process verbatim (SysProcAttr.CmdLine) instead of one composed from
	// Argv — cmd.exe does not parse the \" escaping that composition uses.
	// Ignored on other systems.
	CmdLine    string
	Cols, Rows int
	// TracePath, when set, receives the child's raw output bytes from the
	// first one on — evidence for replaying an emulator mismatch offline.
	TracePath string
}

// ScrollbackLines caps the emulator's scrollback per session.
const ScrollbackLines = 10000
