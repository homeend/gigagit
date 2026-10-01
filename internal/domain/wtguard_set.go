package domain

import (
	"time"

	"github.com/homeend/gigagit/internal/wtguard"
)

// GuardSources is one guard run's snapshot of every non-git source a guard
// needs. Live is unexported, so only domain builds sources: a composition
// root replacing WorktreeGuardSet wraps StandardWorktreeGuards.
type GuardSources struct {
	Policy   InventoryPolicy
	Reserved []string // resolved: absolute, cleaned
	Live     liveView
	Svc      *Service
}

// WorktreeGuardSet composes the guards every "may this worktree be taken?"
// question runs — the inventory, claims and the recycle op alike (one full
// set everywhere). The composition root may replace it; consumers never
// name a provider.
var WorktreeGuardSet = StandardWorktreeGuards

// StandardWorktreeGuards is the default composition. Cheap-before-expensive
// is wtguard.Run's job; this order is the blocked_by order.
func StandardWorktreeGuards(src GuardSources) []wtguard.Guard {
	now := src.Policy.Now
	if now == nil {
		now = time.Now
	}
	return []wtguard.Guard{
		missingGuard{},
		mainGuard{allow: src.Policy.AllowMain},
		detachedGuard{},
		pausedOpGuard{},
		gitLockGuard{},
		reservedGuard{abs: src.Reserved},
		claimGuard{lv: src.Live},
		tuiGuard{lv: src.Live},
		sessionGuard{lv: src.Live},
		dirtyGuard{svc: src.Svc, stale: src.Policy.StaleAfter, now: now},
	}
}
