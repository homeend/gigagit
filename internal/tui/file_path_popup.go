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
	if p.suggesting {
		return p.updateSuggesting(m, msg)
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		rel := repoRelPath(m.currentWorktree, p.input.Value())
		if rel == "" { // nothing to open; keep the popup open
			return m, nil
		}
		if _, ok := p.set[rel]; ok || p.loadErr != nil {
			// Exact tracked file — or no list to validate against: open as before.
			return p.open(m, rel)
		}
		p.suggesting = true
		p.sel = 0
		p.rerank(rel)
		return m, nil
	default:
		p.input.HandleEditKey(msg) // spaces included — do NOT swallow KeySpace
	}
	return m, nil
}

// updateSuggesting handles keys while the suggestion list is visible.
// sel 0 is the open-as-typed escape row; 1..len(matches) are match rows.
func (p *filePathPopup) updateSuggesting(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		p.suggesting = false
		p.matches = nil
		p.sel = 0
		return m, nil
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case tea.KeyDown:
		if p.sel < len(p.matches) {
			p.sel++
		}
		return m, nil
	case tea.KeyPgUp:
		if p.sel -= popupFilterPage; p.sel < 0 {
			p.sel = 0
		}
		return m, nil
	case tea.KeyPgDown:
		if p.sel += popupFilterPage; p.sel > len(p.matches) {
			p.sel = len(p.matches)
		}
		return m, nil
	case tea.KeyEnter:
		if p.sel > 0 {
			return p.open(m, p.matches[p.sel-1].S)
		}
		rel := repoRelPath(m.currentWorktree, p.input.Value())
		if rel == "" {
			return m, nil
		}
		return p.open(m, rel)
	default:
		before := p.input.Value()
		p.input.HandleEditKey(msg)
		if p.input.Value() != before {
			p.sel = 0
			p.rerank(repoRelPath(m.currentWorktree, p.input.Value()))
		}
	}
	return m, nil
}

// open unwinds the popup (and the palette beneath, if any) and opens the
// history or blame surface for rel.
func (p *filePathPopup) open(m Model, rel string) (Model, tea.Cmd) {
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
}

// rerank rebuilds the suggestion list for query (the NORMALIZED input) and
// clamps sel to 0..len(matches).
func (p *filePathPopup) rerank(query string) {
	p.matches = fuzzy.Rank(query, p.all, filePathSuggestLimit)
	if p.sel > len(p.matches) {
		p.sel = len(p.matches)
	}
}

func (p *filePathPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *filePathPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	cw := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString(p.title() + "\n\n")
	b.WriteString(viewField(i18n.T("path: "), p.input, true, cw) + "\n")
	if p.suggesting {
		b.WriteString("\n")
		if p.loading {
			b.WriteString(i18n.T("  (loading…)") + "\n")
		} else {
			rows := make([]winRow, 1+len(p.matches))
			rows[0] = winRow{text: i18n.T("open as typed: %s", strings.TrimSpace(p.input.Value()))}
			for i, mt := range p.matches {
				rows[i+1] = winRow{text: mt.S}
			}
			for i := range rows {
				if i == p.sel {
					rows[i] = winRow{text: "> " + rows[i].text, style: selectedRow}
				} else {
					rows[i].text = "  " + rows[i].text
				}
			}
			visH := len(rows)
			if capRows := popupResolveRowCap(p.maximized, termH, 12); visH > capRows {
				visH = capRows
			}
			for _, ln := range renderWindow(rows, winOpts{w: cw, h: visH, anchor: p.sel}) {
				b.WriteString(ln + "\n")
			}
		}
		b.WriteString("\n" + i18n.T("[enter] open  [↑↓ pgup/pgdn] nav  [esc] back"))
	} else {
		b.WriteString("\n" + i18n.T("[enter] show  [esc] cancel"))
	}
	return modalStyle.Width(inner).Render(b.String()) + "\n"
}
