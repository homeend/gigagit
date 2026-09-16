package tui

import (
	"github.com/homeend/gigagit/internal/i18n"
)

// lockedSideNotice names the side a live selection is pinned to. alt+←/→ post
// it instead of flipping: the range copies ONE side's text, so the lock is the
// contract, not a limitation — and the notice names the way out (esc).
func lockedSideNotice(onOld bool) string {
	if onOld {
		return i18n.T("▸ selection locked to the old side — esc clears it")
	}
	return i18n.T("▸ selection locked to the new side — esc clears it")
}
