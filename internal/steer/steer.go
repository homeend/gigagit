// Package steer carries gg's live-steering protocol: the on-disk inbox a
// `gg session …` command uses to reach a running TUI or `gg web` page.
//
// The channel is deliberately a directory of small JSON files, not a loopback
// control server: no port to discover, no handshake, and a crashed session
// leaves nothing but files the next session sweeps. One inbox per WORKTREE
// (the caller computes the path — this package never touches the environment)
// holds three kinds of file:
//
//	tui.json / web.json     presence; liveness is a FRESH MTIME, not a pid probe
//	cmd-<id>.json           one command, written temp+rename, unlinked by the consumer
//	reply-<id>.json         the consumer's answer, unlinked by the CLI that read it
//
// Every exported function takes the inbox dir explicitly, which is also the
// test seam: a test passes t.TempDir() and needs no environment variable.
package steer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// TUIPresence and WebPresence name the two presence files in an inbox.
	TUIPresence = "tui.json"
	WebPresence = "web.json"

	// MaxCommandBytes caps one command file. A larger file is deleted unread:
	// nothing legitimate approaches 16 KiB, and parsing an unbounded file a
	// stranger dropped in the state dir is not worth the risk.
	MaxCommandBytes = 16 << 10

	// LiveWindow is how fresh a presence file's mtime must be to count as a
	// live session. Sessions re-touch theirs on a 1 s tick, so five seconds is
	// four missed ticks of slack.
	LiveWindow = 5 * time.Second

	cmdPrefix   = "cmd-"
	replyPrefix = "reply-"
	jsonSuffix  = ".json"
	tmpPattern  = "steer-*.tmp" // never matches cmd-*.json / reply-*.json
)

// Target names where a file lives: the working tree (unstaged/untracked), the
// index (staged), or one commit. Values are protocol strings, always English.
type Target struct {
	State  string `json:"state,omitempty"`  // "unstaged" | "staged" | "untracked" | "commit"
	Commit string `json:"commit,omitempty"` // full 40-hex sha when State == "commit"
}

// Line is a landing point in a diff: a 1-based number on one of its two sides.
type Line struct {
	Side string `json:"side"` // "new" | "old"
	No   int    `json:"no"`
}

// Command is one steering command. The consumer sees only file + target +
// side + line — the CLI has already resolved --hunk N into a line number, so
// there is exactly one landing path.
type Command struct {
	ID      string   `json:"id"`
	Cmd     string   `json:"cmd"`            // "navigate" | "reload" | "focus" | "highlight" | "highlight_clear"
	File    string   `json:"file,omitempty"` // repo-relative, git slash form
	Target  *Target  `json:"target,omitempty"`
	Commit  string   `json:"commit,omitempty"` // navigate: reveal this commit, no file
	Line    *Line    `json:"line,omitempty"`
	Step    string   `json:"step,omitempty"`    // "next_note" | "prev_note"
	Sources []string `json:"sources,omitempty"` // reload: "notes" | "status" | "all"
	Panel   string   `json:"panel,omitempty"`   // focus: a protocol panel name
	Side    string   `json:"side,omitempty"`    // highlight: "new" | "old"
	Start   int      `json:"start,omitempty"`
	End     int      `json:"end,omitempty"`
	Tone    string   `json:"tone,omitempty"` // "info" | "warn" | "error"
	Wait    bool     `json:"wait,omitempty"`
}

// Reply is the consumer's answer to one command. Detail and Error are English
// protocol prose an agent parses — never translated.
type Reply struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Presence records a live session. PID is display-only: liveness is decided by
// the file's mtime, which costs one stat, needs no per-OS process API and
// cannot be fooled by pid reuse.
type Presence struct {
	PID      int    `json:"pid"`
	Worktree string `json:"worktree"`
	Started  string `json:"started,omitempty"` // RFC3339
	URL      string `json:"url,omitempty"`     // web.json only
}

// NewID returns a command id that sorts by post time: <unixnano>-<pid>.
func NewID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.Itoa(os.Getpid())
}

// Post writes c into dir's inbox and returns its id, filling in a generated one
// when c.ID is empty. The write is temp+rename, so a consumer draining the dir
// mid-write sees either nothing or the whole file.
func Post(dir string, c Command) (string, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(dir, cmdPrefix+c.ID+jsonSuffix, data); err != nil {
		return "", err
	}
	return c.ID, nil
}

// Drain returns every well-formed command in dir, in name (= post) order, and
// unlinks every cmd-*.json it looked at. A file that is oversize, unparsable or
// carries no id is deleted and dropped: a reply file is named after the id, so
// an id-less command is unanswerable by construction.
func Drain(dir string) []Command {
	names := listPrefixed(dir, cmdPrefix)
	out := make([]Command, 0, len(names))
	for _, n := range names {
		p := filepath.Join(dir, n)
		st, err := os.Stat(p)
		if err != nil || st.Size() > MaxCommandBytes {
			os.Remove(p)
			continue
		}
		data, err := os.ReadFile(p)
		os.Remove(p)
		if err != nil {
			continue
		}
		var c Command
		if json.Unmarshal(data, &c) != nil || c.ID == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// PostReply writes r as reply-<id>.json. A command posted with Wait false gets
// no reply — nothing would ever read it.
func PostReply(dir string, r Reply) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeAtomic(dir, replyPrefix+r.ID+jsonSuffix, data)
}

// AwaitReply polls for reply-<id>.json every 25 ms until timeout, deleting the
// file it read. ok is false when the timeout expired with no answer (the
// command is still in the inbox and may yet be applied) or the file was
// unreadable.
func AwaitReply(dir, id string, timeout time.Duration) (Reply, bool) {
	p := filepath.Join(dir, replyPrefix+id+jsonSuffix)
	deadline := time.Now().Add(timeout)
	for {
		if data, err := os.ReadFile(p); err == nil {
			os.Remove(p)
			var r Reply
			if json.Unmarshal(data, &r) != nil {
				return Reply{}, false
			}
			return r, true
		}
		if !time.Now().Before(deadline) {
			return Reply{}, false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Discard removes every command and reply file in dir. A session sweeps its
// inbox at startup so a crashed session's commands never replay, and a reply
// the CLI abandoned on timeout never outlives the session that wrote it.
// Presence files are NOT leftovers and are left alone.
func Discard(dir string) {
	for _, pfx := range []string{cmdPrefix, replyPrefix} {
		for _, n := range listPrefixed(dir, pfx) {
			os.Remove(filepath.Join(dir, n))
		}
	}
}

// listPrefixed returns the sorted names of dir's <prefix>*.json files.
func listPrefixed(dir, prefix string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, jsonSuffix) {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// writeAtomic creates dir if needed and lands data at dir/name via a temp file
// and a rename, so no reader ever sees a partial file.
func writeAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
