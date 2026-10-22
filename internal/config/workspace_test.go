package config

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/theme"
)

// TestMergeMemberSplit pins the workspace config split: the per-member
// sections come from the member, everything else from the process-wide
// config (spec 2026-10-22-workspaces-design §6).
func TestMergeMemberSplit(t *testing.T) {
	t.Parallel()
	pw := Defaults()
	member := Defaults()
	one := 1
	// Per member.
	member.Worktree.PathTemplate = "/m/{branch}"
	member.Branches.Filter = []branchfilter.Slot{{Name: "member"}}
	member.Tools.Command = []ToolCommand{{Name: "member-tool"}}
	member.Refresh.Status = 77
	member.Versions.MaxAgeDays = 9
	member.Notes.MaxEntries = 3
	// Process-wide.
	member.UI.WheelStep = 42
	member.Console.StepOutKey = "f12"
	member.Tasks.MaxParallel = 3
	member.Agents.StaleAfter = "9h"
	member.Web.Addr = "127.0.0.1:1"
	member.Forge.Prefetch = &one
	member.Debug.LogOperations = true
	member.Themes = map[string]theme.Override{"x": {}}

	got := MergeMember(pw, member)

	for name, pair := range map[string][2]any{
		"Worktree": {got.Worktree, member.Worktree},
		"Branches": {got.Branches, member.Branches},
		"Tools":    {got.Tools, member.Tools},
		"Refresh":  {got.Refresh, member.Refresh},
		"Versions": {got.Versions, member.Versions},
		"Notes":    {got.Notes, member.Notes},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s: want the member's section", name)
		}
	}
	for name, pair := range map[string][2]any{
		"UI":      {got.UI, pw.UI},
		"Console": {got.Console, pw.Console},
		"Tasks":   {got.Tasks, pw.Tasks},
		"Agents":  {got.Agents, pw.Agents},
		"Web":     {got.Web, pw.Web},
		"Forge":   {got.Forge, pw.Forge},
		"Debug":   {got.Debug, pw.Debug},
		"Themes":  {got.Themes, pw.Themes},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s: want the process-wide section", name)
		}
	}
}
