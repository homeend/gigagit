package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// focusedBookmark builds a Bookmark for the file under focus, mirroring the
// shelf-capture precedence. The two-sided diff view is excluded.
func (m Model) focusedBookmark() (model.Bookmark, bool) {
	switch s := m.topLayer().(type) {
	case *historyView:
		if s.sel < 0 || s.sel >= len(s.commits) {
			return model.Bookmark{}, false
		}
		fc := s.commits[s.sel]
		return model.Bookmark{State: model.StateCommitted, Commit: fc.Hash, Path: fc.Path}, true
	case *blameView:
		if s.ctx.path == "" {
			return model.Bookmark{}, false
		}
		if s.ctx.rev == "" { // working-tree blame → the current worktree's working file
			return model.Bookmark{State: model.StateUnstaged, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: s.ctx.path}, true
		}
		return model.Bookmark{State: model.StateCommitted, Commit: s.ctx.rev, Path: s.ctx.path}, true
	}
	if v := m.diffLayer(); v != nil {
		if v.title == "" {
			return model.Bookmark{}, false
		}
		if v.rev != "" {
			return model.Bookmark{State: model.StateCommitted, Commit: v.rev, Path: v.title}, true
		}
		return model.Bookmark{State: model.StateUnstaged, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: v.title}, true
	}
	if v := m.filesView; v != nil {
		if m.filesTreeFocused && m.filesHash != "" {
			if vis := v.visible(); v.sel >= 0 && v.sel < len(vis) && vis[v.sel].path != "" {
				// A file deleted in this commit (status D) has no content at
				// commit:hash:path, so it can't be shelved, bookmarked, or
				// compared — report "no file". The deletion is still viewable
				// via enter (a plain diff).
				if vis[v.sel].status == "D" {
					return model.Bookmark{}, false
				}
				return model.Bookmark{State: model.StateCommitted, Commit: m.filesHash, Path: vis[v.sel].path}, true
			}
		}
		return model.Bookmark{}, false
	}
	switch m.focus {
	case panelFiles:
		if bi, ok := m.backingIndex(panelFiles); ok {
			f := m.status.Files[bi]
			st := model.StateUnstaged
			if f.Kind == model.KindUntracked {
				st = model.StateUntracked
			}
			return model.Bookmark{State: st, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: f.Path}, true
		}
	case panelStaged:
		if bi, ok := m.backingIndex(panelStaged); ok {
			return model.Bookmark{State: model.StateStaged, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: m.status.Files[bi].Path}, true
		}
	}
	return model.Bookmark{}, false
}

// bookmarkToFileRef maps a bookmark's address to a FileRef so the focused
// (left) compare side resolves via domain.ResolveBytes — by address, with no
// pre-resolved blob SHA. A committed bookmark's Commit may be a stash commit.
func bookmarkToFileRef(b model.Bookmark) model.FileRef {
	switch b.State {
	case model.StateShelf:
		return model.FileRef{Source: model.SourceShelf, Locator: b.ShelfID, Path: b.Path}
	case model.StateStaged:
		return model.FileRef{Source: model.SourceStaged, Path: b.Path}
	case model.StateCommitted:
		return model.FileRef{Source: model.SourceCommit, Locator: b.Commit, Path: b.Path}
	default: // unstaged / untracked
		return model.FileRef{Source: model.SourceUnstaged, Path: b.Path}
	}
}

type bookmarkAddedMsg struct {
	bm  model.Bookmark
	err error
}

func (m Model) bookmarkAddCmd(b model.Bookmark) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		stored, err := svc.BookmarkAdd(context.Background(), b)
		return bookmarkAddedMsg{bm: stored, err: err}
	}
}

// commitBookmarkRow offers "Bookmark this commit" on the Commits panel: persist a
// path-less pointer to the selected commit in the bookmark registry. Mirrors
// bookmarkAddRow ("Bookmark this file").
func (m Model) commitBookmarkRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	b := commitBookmark(m.commits[bi])
	return actionRow{
		id:    "commit-bookmark",
		label: "Bookmark this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.bookmarkAddCmd(b)
		},
	}, true
}

// reflogBookmarkRow offers a path-less commit bookmark for the reflog entry
// under the cursor. Anchored on panelReflog selection only.
func (m Model) reflogBookmarkRow() (actionRow, bool) {
	if m.focus != panelReflog || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return actionRow{}, false
	}
	e := m.reflog[bi]
	b := commitBookmark(model.Commit{Hash: e.Hash, Subject: e.Subject})
	return actionRow{
		id:    "reflog-bookmark",
		label: "Bookmark this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.bookmarkAddCmd(b)
		},
	}, true
}

// commitBookmark builds the path-less bookmark for commit c. The subject rides
// in Label as the switcher row's title (Label is not part of the identity).
func commitBookmark(c model.Commit) model.Bookmark {
	return model.Bookmark{
		State:  model.StateCommitted,
		Commit: c.Hash,
		Branch: firstLocalRef(c),
		Path:   "",
		Label:  c.Subject,
	}
}

// firstLocalRef returns the name of the first local-branch ref decorating c, for
// display sugar on a commit bookmark; "" when the commit is no branch's tip.
func firstLocalRef(c model.Commit) string {
	for _, r := range c.Refs {
		if r.Kind == model.RefLocal {
			return r.Name
		}
	}
	return ""
}

// bookmarkAddRow is the menu-only "Bookmark this file" action, present wherever
// a single file is focused. The resolved address is captured at build time.
func (m Model) bookmarkAddRow() (actionRow, bool) {
	b, ok := m.focusedBookmark()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "bookmark-add",
		label: "Bookmark this file",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.bookmarkAddCmd(b)
		},
	}, true
}

// focusedCompareRef freezes the focused file as a FileRef + display label, the
// left ("first pick") side of a compare. Shared by the two compare-against rows.
func (m Model) focusedCompareRef() (model.FileRef, string, bool) {
	b, ok := m.focusedBookmark()
	if !ok {
		return model.FileRef{}, "", false
	}
	return bookmarkToFileRef(b), bookmarkDisplay(b), true
}

// compareAgainstBookmarkRow is the menu-only "Compare against bookmark" action,
// present wherever a single file is focused. The focused file is frozen at build
// time; running it stashes that ref on the Model and opens the bookmark picker
// in compare mode.
func (m Model) compareAgainstBookmarkRow() (actionRow, bool) {
	ref, label, ok := m.focusedCompareRef()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "bookmark-compare",
		label: "Compare against bookmark",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.pendingCompare = &pendingCompare{ref: ref, label: label, target: compareBookmark}
			return m, m.loadBookmarksCmd()
		},
	}, true
}

// compareAgainstShelfRow is the menu-only "Compare against shelf" action: it
// opens the shelf picker in compare mode against the focused file.
func (m Model) compareAgainstShelfRow() (actionRow, bool) {
	ref, label, ok := m.focusedCompareRef()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "shelf-compare-against",
		label: "Compare against shelf",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.pendingCompare = &pendingCompare{ref: ref, label: label, target: compareShelf}
			return m, m.loadShelfCmd(true)
		},
	}, true
}

// compareAgainstWorkingDirRow is the menu-only "Compare against working dir"
// action: it diffs the focused file (commit/staged/shelf source) against the
// same path in the working tree. The second side is fixed (the working file), so
// unlike the bookmark/shelf compares there is no picker — it opens the diff
// directly. Absent when nothing is focused or the focused file is already the
// working-tree version (comparing it against itself is meaningless).
func (m Model) compareAgainstWorkingDirRow() (actionRow, bool) {
	ref, label, ok := m.focusedCompareRef()
	if !ok || ref.Source == model.SourceUnstaged {
		return actionRow{}, false
	}
	return actionRow{
		id:    "compare-working-dir",
		label: "Compare against working dir",
		run: func(m Model) (tea.Model, tea.Cmd) {
			right := model.FileRef{Source: model.SourceUnstaged, Path: ref.Path}
			title := ref.Path + " ↔ working"
			subtitle := label + " → working dir"
			tag := "cmpwd:" + ref.Path
			v := &diffView{title: title, context: subtitle, loading: true, partial: m.diffPartial, long: m.diffLong}
			v.width, _ = m.overlayDims()
			return m.openPickerDiff(v, tag, m.loadCompareTwoRefsCmd(ref, right, title, subtitle, tag))
		},
	}, true
}

// copyToWorkingDirRow is the menu action "Copy to working dir": it writes the
// focused file's resolved bytes into the working tree at its own path, as an
// unstaged change. The write-sibling of compareAgainstWorkingDirRow — same
// focused ref, same guard (absent for a working-tree file or a deletion).
// engine.WriteFile owns the overwrite-or-cancel fork when a differing working
// file already exists; identical bytes are a no-op.
func (m Model) copyToWorkingDirRow() (actionRow, bool) {
	ref, _, ok := m.focusedCompareRef()
	if !ok || ref.Source == model.SourceUnstaged {
		return actionRow{}, false
	}
	return actionRow{
		id:    "copy-working-dir",
		label: "Copy to working dir",
		run: func(m Model) (tea.Model, tea.Cmd) {
			data, err := m.svc.ResolveBytes(context.Background(), ref)
			if err != nil {
				m.statusMsg = "copy to working dir: " + err.Error()
				return m, nil
			}
			return m.startOp(engine.WriteFile{Path: ref.Path, Data: data})
		},
	}, true
}
