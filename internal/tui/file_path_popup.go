package tui

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/fuzzy"
	"github.com/homeend/gigagit/internal/i18n"
)

// filePathKind selects which surface a filePathPopup opens on submit.
type filePathKind int

const (
	filePathHistory filePathKind = iota
	filePathBlame
)

// filePathSuggestLimit caps the fuzzy suggestion list, like fileFinderLimit.
const filePathSuggestLimit = 200

// repoRelPath turns user-typed input into the repo-relative, forward-slashed
// path the git verbs expect. An absolute path inside root is reduced to its
// repo-relative form; anything else is cleaned and slashed as-is. Blank stays
// blank. A path that escapes the repo (../…) falls back to the cleaned input —
// git then reports no history rather than the popup hard-failing.
func repoRelPath(root, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if root != "" && filepath.IsAbs(p) {
		if rel, err := filepath.Rel(root, p); err == nil && !escapesRepo(rel) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func escapesRepo(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// filePathPopup takes a file path and opens the history or blame surface for it.
// Reached from the command palette ("File history" / "File blame"). Mirrors
// gotoCommitPopup but does no pre-validation — a bogus path opens the surface,
// which already renders "(no history)" / a git error.
type filePathPopup struct {
	popupMax
	kind  filePathKind
	input textfield

	// Fuzzy-suggestion state. The tracked-file list loads async on open
	// (distinct msg from the F finder's lsFilesMsg so the two never cross).
	all        []string            // tracked files from LsFiles
	set        map[string]struct{} // exact-match test over all
	loading    bool                // true until filePathLsMsg lands
	loadErr    error               // LsFiles failure → enter falls back to open-as-typed
	suggesting bool                // suggestion list visible below the input
	matches    []fuzzy.Match       // ranked subset of all
	sel        int                 // 0 = open-as-typed escape row, 1..len(matches) = match rows
}

// filePathLsMsg is the async LsFiles result for the file-path popup.
type filePathLsMsg struct {
	paths []string
	err   error
}

func (m Model) openFilePathPopup(kind filePathKind) (Model, tea.Cmd) {
	m = m.pushLayer(&filePathPopup{kind: kind, input: newTextField(""), loading: true})
	return m, m.loadFilePathLsCmd()
}

// loadFilePathLsCmd calls LsFiles off-thread and delivers filePathLsMsg.
func (m Model) loadFilePathLsCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		paths, err := svc.LsFiles(context.Background())
		return filePathLsMsg{paths: paths, err: err}
	}
}

func (p *filePathPopup) title() string {
	if p.kind == filePathBlame {
		return i18n.T("File blame")
	}
	return i18n.T("File history")
}

func (p *filePathPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		rel := repoRelPath(m.currentWorktree, p.input.Value())
		if rel == "" { // nothing to open; keep the popup open
			return m, nil
		}
		// Unwind this popup and, if the palette launched us, the palette too — the
		// full-screen surface must open over the base, not a stale popup.
		m = m.popLayer()
		if _, ok := m.topLayer().(*commandPalette); ok {
			m = m.popLayer()
		}
		ctx := navContext{path: rel, rev: ""}
		if p.kind == filePathBlame {
			bv := newBlameView(ctx)
			m = m.pushLayer(bv)
			return m, m.loadBlameCmd(ctx, bv.tag)
		}
		hv := newHistoryView(ctx)
		m = m.pushLayer(hv)
		return m, m.loadHistoryListCmd(ctx, hv.listTag)
	default:
		p.input.HandleEditKey(msg) // spaces included — do NOT swallow KeySpace
	}
	return m, nil
}

func (p *filePathPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *filePathPopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(p.title() + "\n\n")
	b.WriteString(viewField(i18n.T("path: "), p.input, true, popupContentWidth(w)) + "\n")
	b.WriteString("\n" + i18n.T("[enter] show  [esc] cancel"))
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
