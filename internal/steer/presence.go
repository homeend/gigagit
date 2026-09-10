package steer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Touch records (or refreshes) a live session's presence. The common case —
// the session's 1 s tick — is a single os.Chtimes on an existing file: liveness
// is the mtime, so re-marshalling the same payload every second would be pure
// waste. The file is (re)written only when it is missing, which also covers the
// case where a CLI Live check swept it while this session stalled past the
// window.
func Touch(dir, name string, p Presence) error {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		now := time.Now()
		if err := os.Chtimes(path, now, now); err == nil {
			return nil
		}
		// Fall through: the file exists but its clock could not be set (a
		// read-only remount, a racing sweep) — rewrite it instead.
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return writeAtomic(dir, name, data)
}

// Live reports the presence recorded in dir/name when its mtime is under
// LiveWindow old. A presence older than that belonged to a session that
// crashed or was SIGKILLed; Live removes it, so the next caller does not even
// stat it. An unparsable presence is removed the same way: leaving it behind
// would only let Touch keep refreshing its mtime forever with content nothing
// can ever read.
func Live(dir, name string) (Presence, bool) {
	path := filepath.Join(dir, name)
	st, err := os.Stat(path)
	if err != nil {
		return Presence{}, false
	}
	if time.Since(st.ModTime()) > LiveWindow {
		os.Remove(path)
		return Presence{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Presence{}, false
	}
	var p Presence
	if json.Unmarshal(data, &p) != nil {
		os.Remove(path)
		return Presence{}, false
	}
	return p, true
}

// Remove deletes a presence file. A session calls it on clean exit and on a
// repo switch; a second call is a silent no-op.
func Remove(dir, name string) { os.Remove(filepath.Join(dir, name)) }
