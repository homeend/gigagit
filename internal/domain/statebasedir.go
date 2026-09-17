package domain

import (
	"os"
	"path/filepath"
	"runtime"
)

// stateBaseDir resolves <state>/gg/<kind> cross-platform. "" when no home or
// state dir exists, which every caller reads as "this store is disabled".
//
// Extracted from what were eight byte-identical copies (bookmarkBaseDir,
// notesBaseDir, prefixBaseDir, previewBaseDir, profileBaseDir,
// reviewsBaseDir, searchBaseDir, shelfBaseDir) differing only in their kind
// string — the project rule "a new store MUST check XDG_STATE_HOME first"
// only holds by construction once there is exactly one place it is written.
func stateBaseDir(kind string) string {
	// An explicitly-set $XDG_STATE_HOME wins on every platform (it is a
	// deliberate override — and the only way tests can isolate state on
	// Windows); %LocalAppData% is the ambient Windows default.
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", kind)
	}
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", kind)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", kind)
}
