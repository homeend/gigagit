package tui

// panelSelKey returns a stable identity for the currently-selected row of p,
// so a refresh that reorders or removes rows can restore the selection by
// identity rather than by a now-meaningless index.
// Empty string = no stable identity (out-of-range, empty panel, WIP row).
func (m Model) panelSelKey(p panel) string {
	return m.rowKeyAt(p, m.sel[p])
}

// rowKeyAt returns the stable identity for display row i of panel p.
// i is a display index (0..panelLen(p)-1). Used by restorePanelSel to scan
// the updated list and find a key match.
//
// Implementation note: m.sel[p] is a display index, not a backing index.
// displayIndices(p) maps display positions → backing slice positions (after
// membership filter, text filter, and sort). We resolve i → u (backing) and
// then index the appropriate slice. This ensures identity and length always
// agree with panelLen (which is also len(displayIndices(p))).
//
// Per-panel identity choices:
//   - panelBranches  → Branch.Name (stable across sort/filter)
//   - panelRemotes   → RemoteBranch.Name (e.g. "origin/main")
//   - panelWorktrees → Worktree.Path (absolute path; unique)
//   - panelTags      → Tag.Name
//   - panelFiles     → FileStatus.Path via status.Files[u] (both Files and
//     Staged panels back onto the same status.Files slice; memberOf filters
//     each panel's subset — we index through displayIndices so u is already
//     the correct backing index into that slice)
//   - panelStaged    → same as panelFiles (status.Files[u].Path)
//   - panelCommits   → Commit.Hash via commits[u-wipCount()]; WIP pseudo-rows
//     have no stable hash so they return "" (restore degrades to index-clamp)
//   - panelReflog    → ReflogEntry.Hash (full SHA; stable reflog identity)
func (m Model) rowKeyAt(p panel, i int) string {
	idx := m.displayIndices(p)
	if i < 0 || i >= len(idx) {
		return ""
	}
	u := idx[i] // backing (or unified-commits) index
	switch p {
	case panelBranches:
		return m.branches[u].Name
	case panelRemotes:
		return m.remoteBranches[u].Name
	case panelWorktrees:
		return m.worktrees[u].Path
	case panelTags:
		return m.tags[u].Name
	case panelFiles, panelStaged:
		// Both panels back onto the full m.status.Files slice; displayIndices
		// applies the membership filter (inFilesPanel / inStagedPanel) before
		// returning u, so u is always a valid index into status.Files.
		return m.status.Files[u].Path
	case panelCommits:
		// The unified commit list is: wipRows (0..wipCount-1) ++ commits.
		// WIP pseudo-rows (working tree / staged) have no commit hash;
		// return "" so restore falls back to index-clamping for them.
		if m.isWipRow(u) {
			return ""
		}
		return m.commits[u-m.wipCount()].Hash
	case panelReflog:
		return m.reflog[u].Hash
	}
	return ""
}

// restorePanelSel re-finds key in p's current (post-refresh) list and sets
// m.sel[p] to the matching display index. If key is empty or not found,
// m.sel[p] is clamped to the last valid row (or 0 for an empty panel).
func (m Model) restorePanelSel(p panel, key string) Model {
	n := m.panelLen(p)
	if n == 0 {
		m.sel[p] = 0
		return m
	}
	if key != "" {
		for i := 0; i < n; i++ {
			if m.rowKeyAt(p, i) == key {
				m.sel[p] = i
				return m
			}
		}
	}
	// Key not found or empty: clamp existing index to valid range.
	if m.sel[p] >= n {
		m.sel[p] = n - 1
	}
	if m.sel[p] < 0 {
		m.sel[p] = 0
	}
	return m
}
