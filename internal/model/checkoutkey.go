package model

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// CheckoutKey is a checkout (worktree) path's IDENTITY: the one value two
// spellings of the same directory share — cleaned, in slash form, and
// case-folded where the platform's paths are case-insensitive (Windows,
// macOS). It is for comparing and keying only: it never reaches the
// filesystem, a git invocation or the screen — those take the path as it
// was listed or typed. Being a distinct type, a raw path cannot be
// compared with a key by mistake; the compiler asks for KeyOf.
type CheckoutKey string

// KeyOf is path's CheckoutKey; "" for "" (an unset identity).
func KeyOf(path string) CheckoutKey {
	if path == "" {
		return ""
	}
	s := filepath.ToSlash(filepath.Clean(path))
	if caseInsensitivePaths.Load() {
		s = strings.ToLower(s)
	}
	return CheckoutKey(s)
}

// SamePath reports whether two paths name the same checkout (KeyOf equal).
func SamePath(a, b string) bool { return KeyOf(a) == KeyOf(b) }

// caseInsensitivePaths is the fold rule: per platform by default (the
// filesystems gg meets there fold case). Settable for tests on a platform
// that does not fold, so the rule is exercised everywhere.
var caseInsensitivePaths atomic.Bool

func init() { caseInsensitivePaths.Store(runtime.GOOS == "windows" || runtime.GOOS == "darwin") }

// CaseInsensitivePaths reports whether KeyOf folds case.
func CaseInsensitivePaths() bool { return caseInsensitivePaths.Load() }

// SetCaseInsensitivePaths overrides the fold rule (tests; a sequential
// test restores it).
func SetCaseInsensitivePaths(on bool) { caseInsensitivePaths.Store(on) }
