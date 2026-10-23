package config

// MergeMember is the workspace config split (spec
// docs/superpowers/specs/2026-10-22-workspaces-design.md §6): the sections
// that describe ONE repository — its worktree and branch templates and
// filters, its tool commands, its refresh cadence, its version and note
// policies — come from member (that repository's own overlay), while the
// sections that describe the one terminal, keyboard and process — [ui],
// [console], [tasks], [agents], [web], [forge], [debug], [themes] — stay
// processWide's (the workspace's settings repository). Neither argument is
// modified.
func MergeMember(processWide, member Config) Config {
	out := processWide
	out.Worktree = member.Worktree
	out.Branches = member.Branches
	out.Tools = member.Tools
	out.Refresh = member.Refresh
	out.Versions = member.Versions
	out.Notes = member.Notes
	return out
}
