package domain

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/model"
)

// WorkspaceInfo is the open workspace as its owner tells the web page (spec
// 2026-10-31-web-workspaces §2): the terminal under gg/gg --web, a small owner
// in internal/web under standalone gg web. It lives here because tui does not
// import web. nil means no workspace is open.
type WorkspaceInfo struct {
	Name         string
	Live         int // index of the member on screen
	SettingsRepo string
	Members      []WorkspaceMember
}

// WorkspaceMember is one repository of the open workspace. Svc is the owner's
// own service, so caches and the repo gate are shared; nil for a missing member.
type WorkspaceMember struct {
	Path, Label string
	Missing     bool
	LastViewed  string
	Svc         *Service
}

// WorkspaceRequest is what the page asks of the owner.
type WorkspaceRequest struct {
	Kind string   // "open", "leave", "edit", "launch"
	Edit string   // Kind "edit": "new", "rename", "delete", "add", "remove", "move", "settings"
	Name string   // the workspace; "" on launch = open the repository alone
	Args []string // the edit's arguments
	Drop string   // "" = ask, "keep" or "stop" the running sessions of a dropped member
}

// WorkspaceReply is the owner's answer. A non-empty DropSessions means nothing
// happened: the page asks and repeats the request with Drop set.
type WorkspaceReply struct {
	DropSessions []SessionInfo
	Status       string
}

// WorkspaceQuestion is the pending launch question: this repository is a member
// of the listed workspaces, and none was asked for.
type WorkspaceQuestion struct {
	Repo string
	Rows []WorkspaceQuestionRow
}

// WorkspaceQuestionRow is one workspace the launch question offers.
type WorkspaceQuestionRow struct{ Name, Label string }

// DefaultWorktree is where a switch into a member lands when the caller names
// no worktree: lastViewed while it is listed, else the first non-bare entry
// (the main checkout, or the first worktree of a bare repository). "" when the
// list holds neither.
func DefaultWorktree(wts []model.Worktree, lastViewed string) string {
	first := ""
	for _, w := range wts {
		if lastViewed != "" && model.SamePath(w.Path, lastViewed) {
			return filepath.Clean(w.Path)
		}
		if first == "" && !w.Bare {
			first = filepath.Clean(w.Path)
		}
	}
	return first
}

// RunningSessionsIn is the running sessions in the listed worktrees: the ones a
// change dropping that member asks about.
func RunningSessionsIn(wts []model.Worktree) []SessionInfo {
	return runningSessionsAmong(Sessions().List(), wts)
}

func runningSessionsAmong(infos []SessionInfo, wts []model.Worktree) []SessionInfo {
	keys := make(map[model.CheckoutKey]bool, len(wts))
	for _, w := range wts {
		keys[model.KeyOf(w.Path)] = true
	}
	var out []SessionInfo
	for _, info := range infos {
		if info.State == SessionRunning && keys[model.KeyOf(info.Dir)] {
			out = append(out, info)
		}
	}
	return out
}
