package tui

import (
	"context"
	pathpkg "path"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// fileFinderLimit is the most fuzzy matches F's window lists; keeps ranking
// O(n log limit) even over 100k-path repos.
const fileFinderLimit = 200

// lsFilesMsg is the async result of the LsFiles domain call.
type lsFilesMsg struct {
	paths []string
	err   error
}

// loadLsFilesCmd returns a Cmd that calls LsFiles off-thread and delivers
// lsFilesMsg back to Update.
func (m Model) loadLsFilesCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		paths, err := svc.LsFiles(context.Background())
		return lsFilesMsg{paths: paths, err: err}
	}
}

// worktreeFileRows is the actions on one file of F's working-tree window
// (enter or .). The file is on disk, so viewing and editing act on it; an
// untracked file has no history, so the git rows are tracked-only. History,
// blame and the diff push over the window (esc returns to it).
func (m Model) worktreeFileRows(path string, untracked bool) []actionRow {
	rows := []actionRow{{
		id:    "ff-view",
		label: i18n.T("View file content"),
		// The working-tree version in the full-screen viewer — an open file,
		// reloaded when the disk changes. esc (or ctrl+]) returns here.
		run: func(m Model) (tea.Model, tea.Cmd) { return m.openFileViewer(path, 0) },
	}}
	if !untracked {
		rows = append(rows, actionRow{
			id:    "ff-diff",
			label: i18n.T("Diff (HEAD ↔ working tree)"),
			run:   func(m Model) (tea.Model, tea.Cmd) { return m.openWorktreeDiff(path) },
		}, actionRow{
			id:    "ff-history",
			label: i18n.T("History"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				ctx := navContext{path: path}
				hv := newHistoryView(ctx)
				m = m.pushLayer(hv)
				return m, m.loadHistoryListCmd(ctx, hv.listTag)
			},
		}, actionRow{
			id:    "ff-blame",
			label: i18n.T("Blame"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				ctx := navContext{path: path}
				bv := newBlameView(ctx)
				m = m.pushLayer(bv)
				return m, m.loadBlameCmd(ctx, bv.tag)
			},
		})
	}
	rows = append(rows, actionRow{
		id:    "ff-editor",
		label: i18n.T("Edit in editor"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m, m.editFileCmd(path) },
	}, actionRow{
		id:    "ff-copy-name",
		label: i18n.T("Copy file name"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			name := pathpkg.Base(path)
			return m, m.copyToClipboardCmd(i18n.T("Copied %s", name), name)
		},
	}, actionRow{
		id:    "ff-copy-path",
		label: i18n.T("Copy path"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.copyToClipboardCmd(i18n.T("Copied %s", path), path)
		},
	}, actionRow{
		id:    "ff-copy-abspath",
		label: i18n.T("Copy absolute path"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			abs := m.absFilePath("", path)
			return m, m.copyToClipboardCmd(i18n.T("Copied %s", abs), abs)
		},
	})
	if !untracked {
		rows = append(rows, actionRow{
			id:    "ff-commits-touching",
			label: i18n.T("Commits touching this"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.closeFilesView()
				m.commitFilter = commitFilterFields{Paths: []string{path}}
				m = m.focusCommitsPanel()
				return m.startFeedReload()
			},
		})
	}
	return rows
}

// openWorktreeDiff opens path's HEAD ↔ working tree diff over the window.
func (m Model) openWorktreeDiff(path string) (tea.Model, tea.Cmd) {
	// HEAD is resolved to a full sha HERE, on the Update thread, before it
	// becomes an Endpoint: CacheTag() returns Hash verbatim and is the
	// session diff-cache key, so the literal "HEAD" there would key the
	// cache on a name that moves (see resolveHeadEndpoint).
	left, err := m.resolveHeadEndpoint()
	if err != nil {
		// Zero-commit repo or a failed resolve: open the view on its error
		// rather than comparing against nothing.
		m = m.pushLayer(&diffView{title: path, context: i18n.T("HEAD ↔ working tree"), err: err})
		m.diffNav = diffNavNone
		m.diffTag = ""
		return m, nil
	}
	right := model.WorkTreeEndpoint()
	m = m.pushLayer(&diffView{
		title:   path,
		context: i18n.T("HEAD ↔ working tree"),
		loading: true,
		partial: m.diffPartial,
		long:    m.diffLong,
	})
	m.diffNav = diffNavNone
	// Byte-identical to the tag loadCompareDiffCmd computes internally, or
	// the diffMsg would be dropped as stale by the handler's gate.
	m.diffTag = "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + path
	return m, m.loadCompareDiffCmd(left, right, contentLine{path: path})
}

// wtActionMenu opens the actions of the file under the window's cursor.
func (m Model) wtActionMenu() Model {
	path := m.wtSelected()
	if path == "" {
		return m
	}
	rows := m.worktreeFileRows(path, m.wtFiles.untracked[path])
	if link, ok := m.contextFileLinkRow(); ok {
		rows = append(rows, link)
	}
	m.actionMenu = &actionMenu{rows: rows}
	return m
}

// windowBounds returns [start, end) for a window of height h anchored on sel
// within a list of length n.
func windowBounds(n, sel, h int) (int, int) {
	if h >= n {
		return 0, n
	}
	start := sel - h/2
	if start < 0 {
		start = 0
	}
	end := start + h
	if end > n {
		end = n
		start = end - h
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// rangeSlice returns indices [start, end).
func rangeSlice(start, end int) []int {
	out := make([]int, end-start)
	for i := range out {
		out[i] = start + i
	}
	return out
}
