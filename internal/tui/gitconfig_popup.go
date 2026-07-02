package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/gitconfdocs"
	"github.com/homeend/gigagit/internal/model"
)

// gitConfigPopup is the Settings → "Git config explorer": every key git
// knows (git help -c), the explicitly-set local/global values, and — for
// curated keys (internal/gitconfdocs) — the real default and a description.
// Navigation-first like the repo switcher: / filters (move-while-typing),
// z cycles display modes, esc closes. Curated rows edit in place: l/g open
// a scope editor (option picker or text field per the doc's Kind), u a
// set-scopes-only unset chooser; non-curated rows stay read-only.
type gitConfigPopup struct {
	rows      []model.GitConfigRow
	loading   bool
	query     string
	filtering bool
	sel       int
	mode      dispMode
	hscroll   int
	edit      *configEdit // in-popup editor state; nil = browsing
}

// configEdit is the in-popup editor state for one curated key: an option
// list for bool/enum kinds, a text field for string/int, or the unset-scope
// chooser. nil edit = browsing.
type configEdit struct {
	key      string
	doc      *gitconfdocs.Doc
	global   bool
	unset    bool     // the unset chooser (options built from set scopes)
	options  []string // option-list editors (incl. unset chooser labels)
	optSel   int
	field    textfield // string/int kinds
	useField bool
}

// gitConfigRowsMsg carries the merged rows; gen guards repo switches and
// reopen races. summary is set by the post-write re-read (gitConfigWriteCmd)
// so the status line reports the op result alongside the fresh rows. health
// is the repo-health snapshot chained INSIDE the same write, taken after the
// write lands — nil when this msg isn't a write result (or the health read
// itself failed) — so the notice/Settings health state never races the
// config write it's supposed to observe.
type gitConfigRowsMsg struct {
	gen     int
	rows    []model.GitConfigRow
	err     error
	summary string
	health  *model.RepoHealth
}

// openGitConfigExplorer pushes the loading popup and reads the rows off the
// UI thread.
func (m Model) openGitConfigExplorer() (Model, tea.Cmd) {
	m.gitConfigGen++
	m = m.pushLayer(&gitConfigPopup{loading: true})
	svc := m.svc
	gen := m.gitConfigGen
	return m, func() tea.Msg {
		rows, err := svc.GitConfigRows(context.Background())
		return gitConfigRowsMsg{gen: gen, rows: rows, err: err}
	}
}

// moveSel moves the cursor by d, clamped to the filtered view.
func (p *gitConfigPopup) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visible()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// visible returns the filtered rows (case-insensitive substring over the key
// and both values).
func (p *gitConfigPopup) visible() []model.GitConfigRow {
	if p.query == "" {
		return p.rows
	}
	q := strings.ToLower(p.query)
	out := make([]model.GitConfigRow, 0, len(p.rows))
	for _, r := range p.rows {
		if strings.Contains(strings.ToLower(r.Key), q) ||
			strings.Contains(strings.ToLower(r.LocalValue), q) ||
			strings.Contains(strings.ToLower(r.GlobalValue), q) {
			out = append(out, r)
		}
	}
	return out
}

// update handles all keys while the explorer is open. It swallows everything
// (no fallthrough to global handlers), mirroring repoPopup: plain keys
// navigate, `/` enters a filter sub-mode where runes (including `z`) type a
// query until esc/enter, and l/g/u open the in-place editor on curated rows.
// While the editor is open ALL keys route to it (the filter `/` etc. are
// inert).
func (p *gitConfigPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.edit != nil {
		return p.updateEdit(m, msg)
	}
	if p.filtering {
		// Arrows/pages move the selection live while typing (no cursor reset),
		// like the commit filter; j/k stay query text.
		if filterMotion(msg, p.moveSel, popupFilterPage) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.query, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false // commit: keep the filter, leave input mode
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
			p.sel = 0
		case tea.KeySpace:
			p.query += " "
			p.sel = 0
		case tea.KeyRunes:
			p.query += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	// Navigation mode. Display-mode + pan keys act here (query chars while filtering).
	switch msg.String() {
	case "z":
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
		return m, nil
	case tea.KeyUp:
		p.moveSel(-1)
		return m, nil
	case tea.KeyDown:
		p.moveSel(1)
		return m, nil
	case tea.KeyPgUp:
		p.moveSel(-popupFilterPage)
		return m, nil
	case tea.KeyPgDown:
		p.moveSel(popupFilterPage)
		return m, nil
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "j":
			p.moveSel(1)
		case "k":
			p.moveSel(-1)
		case "l", "g":
			return p.openSetEditor(m, msg.String() == "g")
		case "u":
			return p.openUnsetChooser(m)
		}
		return m, nil
	}
	return m, nil
}

// selectedRowData returns the row under the cursor in the filtered view, or
// false when the view is empty.
func (p *gitConfigPopup) selectedRowData() (model.GitConfigRow, bool) {
	vis := p.visible()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.GitConfigRow{}, false
	}
	return vis[p.sel], true
}

// readOnlyRefusal is the statusMsg for an edit key on a non-curated row.
const readOnlyRefusal = "read-only: not a curated key (edit via git config)"

// openSetEditor opens the set editor for the selected curated row at the
// chosen scope: an option picker for bool/enum kinds (pre-selected on the
// current scope value when set, else the curated default), a text field for
// string/int (pre-filled with the current scope value, empty when unset).
func (p *gitConfigPopup) openSetEditor(m Model, global bool) (Model, tea.Cmd) {
	row, ok := p.selectedRowData()
	if !ok {
		return m, nil
	}
	doc := gitconfdocs.Lookup(row.Key)
	if doc == nil {
		m.statusMsg = readOnlyRefusal
		return m, nil
	}
	cur, isSet := row.LocalValue, row.LocalSet
	if global {
		cur, isSet = row.GlobalValue, row.GlobalSet
	}
	if !isSet {
		cur = ""
	}
	e := &configEdit{key: doc.Key, doc: doc, global: global}
	switch doc.Kind {
	case gitconfdocs.KindBool:
		e.options = []string{"true", "false"}
	case gitconfdocs.KindEnum:
		e.options = append([]string(nil), doc.Options...)
	default: // KindString, KindInt
		e.useField = true
		e.field = newTextField(cur)
	}
	if len(e.options) > 0 {
		want := doc.Default
		if isSet {
			want = cur
		}
		for i, o := range e.options {
			if o == want {
				e.optSel = i
				break
			}
		}
	}
	p.edit = e
	return m, nil
}

// Unset chooser labels; editEnter matches on them to pick the scope.
const (
	unsetLocalLabel  = "Unset local"
	unsetGlobalLabel = "Unset global"
	unsetCancelLabel = "Cancel"
)

// openUnsetChooser opens the unset chooser for the selected curated row,
// offering ONLY the scopes that are actually set (plus Cancel). Nothing set
// → a statusMsg refusal, no chooser.
func (p *gitConfigPopup) openUnsetChooser(m Model) (Model, tea.Cmd) {
	row, ok := p.selectedRowData()
	if !ok {
		return m, nil
	}
	doc := gitconfdocs.Lookup(row.Key)
	if doc == nil {
		m.statusMsg = readOnlyRefusal
		return m, nil
	}
	var opts []string
	if row.LocalSet {
		opts = append(opts, unsetLocalLabel)
	}
	if row.GlobalSet {
		opts = append(opts, unsetGlobalLabel)
	}
	if len(opts) == 0 {
		m.statusMsg = "nothing to unset"
		return m, nil
	}
	opts = append(opts, unsetCancelLabel)
	p.edit = &configEdit{key: doc.Key, doc: doc, unset: true, options: opts}
	return m, nil
}

// updateEdit routes every key while the editor is open: esc cancels back to
// browsing, enter saves, up/down (j/k) move an option list's selection, and
// everything else edits the text field (KindInt filtered to digits plus a
// leading '-').
func (p *gitConfigPopup) updateEdit(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	e := p.edit
	switch msg.Type {
	case tea.KeyEsc:
		p.edit = nil
		return m, nil
	case tea.KeyEnter:
		return p.editEnter(m)
	}
	if e.useField {
		if e.doc != nil && e.doc.Kind == gitconfdocs.KindInt {
			switch msg.Type {
			case tea.KeyRunes:
				for _, r := range msg.Runes {
					leadMinus := r == '-' && e.field.cursor == 0 && !strings.HasPrefix(e.field.Value(), "-")
					if (r >= '0' && r <= '9') || leadMinus {
						e.field.insert([]rune{r})
					}
				}
				return m, nil
			case tea.KeySpace:
				return m, nil // an int never contains a space
			}
		}
		e.field.HandleEditKey(msg)
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		if e.optSel > 0 {
			e.optSel--
		}
	case "down", "j":
		if e.optSel < len(e.options)-1 {
			e.optSel++
		}
	}
	return m, nil
}

// editEnter builds the SetGitConfig op from the editor state and dispatches
// it (write + rows re-read, chained with a repo-health re-read INSIDE the
// same cmd — a config write can change repo health, e.g. unsetting
// fetch.writeCommitGraph re-arms the commit-graph notice check). The health
// read is chained after the write, not run in a separate tea.Batch cmd, so
// it always observes the post-write config instead of racing it. The editor
// closes immediately; the popup shows the refreshed rows when the write's
// re-read lands.
func (p *gitConfigPopup) editEnter(m Model) (Model, tea.Cmd) {
	e := p.edit
	var op engine.SetGitConfig
	if e.unset {
		switch e.options[e.optSel] {
		case unsetLocalLabel:
			op = engine.SetGitConfig{Key: e.key, Unset: true}
		case unsetGlobalLabel:
			op = engine.SetGitConfig{Key: e.key, Unset: true, Global: true}
		default: // Cancel
			p.edit = nil
			return m, nil
		}
	} else {
		var val string
		if e.useField {
			val = strings.TrimSpace(e.field.Value())
		} else {
			val = e.options[e.optSel]
		}
		op = engine.SetGitConfig{Key: e.key, Value: val, Global: e.global}
	}
	p.edit = nil
	return m, m.gitConfigWriteCmd(op)
}

// gitConfigWriteCmd runs one config write synchronously (the stageCmd
// pattern — fast + decision-free, no busy-line machinery), re-reads the rows
// so the popup refreshes in the same message, and — now that the write has
// landed — also re-reads repo health so notices/Settings observe the
// post-write config instead of a stale snapshot from a concurrent read. It
// reuses the CURRENT generation: the refresh lands unless the popup was
// reopened/re-rooted.
func (m Model) gitConfigWriteCmd(op engine.SetGitConfig) tea.Cmd {
	svc := m.svc
	gen := m.gitConfigGen
	return func() tea.Msg {
		res, err := svc.Execute(context.Background(), op, nil, nil)
		if err != nil {
			return gitConfigRowsMsg{gen: gen, err: err}
		}
		rows, rerr := svc.GitConfigRows(context.Background())
		var health *model.RepoHealth
		if h, herr := svc.RepoHealth(context.Background()); herr == nil {
			health = &h
		}
		return gitConfigRowsMsg{gen: gen, rows: rows, err: rerr, summary: res.Summary, health: health}
	}
}

// render composites the explorer over the layer beneath.
func (p *gitConfigPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// Column-width tuning: the ideal split is key 36 | local 18 | global 18 |
// default (rest). On a narrower terminal (the default 80-col test terminal
// included) that ideal sum doesn't fit the popup's text width, so key gives
// way first (a shortened key is still identifiable, and z switches to
// wrap/scroll to see the rest), then local/global shrink together; the
// default column — usually a short literal like "false" or "—" — always
// keeps a small floor so it never disappears entirely.
const (
	gitConfigKeyIdeal    = 36
	gitConfigLocalIdeal  = 18
	gitConfigGlobalIdeal = 18
	gitConfigColSep      = 1
	gitConfigKeyFloor    = 22
	gitConfigSideFloor   = 8
	gitConfigDefaultMin  = 4
)

// gitConfigColWidths computes the four column widths for the given text
// width, shrinking from the ideal split when necessary (see the const block
// above for the shrink order and rationale).
func gitConfigColWidths(textW int) (keyW, localW, globalW, defaultW int) {
	keyW, localW, globalW = gitConfigKeyIdeal, gitConfigLocalIdeal, gitConfigGlobalIdeal
	sep := 3 * gitConfigColSep
	need := func() int { return keyW + localW + globalW + sep + gitConfigDefaultMin }
	for need() > textW && keyW > gitConfigKeyFloor {
		keyW--
	}
	for need() > textW && localW > gitConfigSideFloor {
		localW--
	}
	for need() > textW && globalW > gitConfigSideFloor {
		globalW--
	}
	defaultW = textW - keyW - localW - globalW - sep
	if defaultW < 1 {
		defaultW = 1
	}
	return
}

// gitConfigDefaultCell renders the default-value cell: the curated default
// for a known key, or an em dash for one gg doesn't curate.
func gitConfigDefaultCell(key string, width int) string {
	def := "—"
	if doc := gitconfdocs.Lookup(key); doc != nil {
		def = doc.Default
	}
	return padRight(truncate(def, width), width)
}

// gitConfigRowText lays out one row's four columns at the given widths.
func gitConfigRowText(r model.GitConfigRow, keyW, localW, globalW, defaultW int) string {
	key := padRight(truncate(r.Key, keyW), keyW)
	local := configCell(r.LocalValue, r.LocalSet, localW)
	global := configCell(r.GlobalValue, r.GlobalSet, globalW)
	def := gitConfigDefaultCell(r.Key, defaultW)
	return key + " " + local + " " + global + " " + def
}

// box draws the explorer box (modal box only). While the in-place editor is
// open it shows the editor instead of the list.
func (p *gitConfigPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupWideInnerWidth(w)
	textW := popupTextWidth(inner)

	if p.edit != nil {
		return p.editBox(inner, textW)
	}

	title := "Git config"
	if p.loading {
		title += " (⏳ loading…)"
	} else {
		title += fmt.Sprintf(" (%d keys)", len(p.rows))
	}
	switch {
	case p.filtering:
		title += "  /" + p.query + "█"
	case p.query != "":
		title += "  /" + p.query
	default:
		title += "   (press / to filter)"
	}

	keyW, localW, globalW, defaultW := gitConfigColWidths(textW)
	header := padRight("Key", keyW) + " " + padRight("Local", localW) + " " + padRight("Global", globalW) + " " + padRight("Default", defaultW)

	vis := p.visible()
	var bodyLines []string
	switch {
	case p.loading:
		bodyLines = []string{padRight("  loading…", textW)}
	case len(vis) == 0:
		bodyLines = []string{padRight("  (no match)", textW)}
	default:
		wr := make([]winRow, len(vis))
		for i, r := range vis {
			var st lipgloss.Style
			if i == p.sel {
				st = selectedRow
			}
			wr[i] = winRow{
				text:  gitConfigRowText(r, keyW, localW, globalW, defaultW),
				style: st,
			}
			// Skip the decorator on the selected row: its inner reset would
			// cancel selectedRow's reverse highlight mid-row (the commits
			// panel does the same skip, see view.go's decorator gating).
			if i != p.sel {
				wr[i].decorate = configRowDecorator(r, keyW, localW, globalW)
			}
		}
		// Height budget: capped like the session-errors viewer so the popup
		// stays on-screen no matter how many keys git knows about.
		capRows := termH - 12
		if capRows < 3 {
			capRows = 3
		}
		h := len(vis)
		if h > capRows {
			h = capRows
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	// The selected row's curated description (blank for a non-curated key).
	// Wrapped (not hard-truncated) so a long description — up to a few
	// lines — stays fully readable instead of losing its tail to "…".
	var descLine string
	if p.sel >= 0 && p.sel < len(vis) {
		if doc := gitconfdocs.Lookup(vis[p.sel].Key); doc != nil {
			descLine = doc.Desc
		}
	}
	descLines := []string{""}
	if descLine != "" {
		descLines = wrapWidth(descLine, textW, 3)
	}

	hint := []string{"[l] set local", "[g] set global", "[u] unset", "[/] filter", "[z] mode", "[esc] close"}
	parts := []string{title, "", header}
	parts = append(parts, bodyLines...)
	parts = append(parts, "")
	parts = append(parts, descLines...)
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// editBox draws the in-place editor: the key + scope title, then the option
// list (selectedRow highlight) or the text field, the curated description,
// and the save/cancel hint.
func (p *gitConfigPopup) editBox(inner, textW int) string {
	e := p.edit
	scope := "local"
	if e.global {
		scope = "global"
	}
	title := "Set " + e.key + " (" + scope + ")"
	if e.unset {
		title = "Unset " + e.key
	}
	parts := []string{title, ""}
	if e.useField {
		parts = append(parts, viewField("value: ", e.field, true, textW))
	} else {
		for i, opt := range e.options {
			row := "  " + opt
			if i == e.optSel {
				row = selectedRow.Render("> " + opt)
			}
			parts = append(parts, row)
		}
	}
	if e.doc != nil && e.doc.Desc != "" {
		parts = append(parts, "")
		parts = append(parts, wrapWidth(e.doc.Desc, textW, 3)...)
	}
	parts = append(parts, "", "[enter] save  [esc] cancel")
	return popupBox(inner, strings.Join(parts, "\n"))
}

// configCell renders one scope cell's PLAIN text: the value, or "(unset)".
// Deliberately unstyled — styling is applied post-slice by
// configRowDecorator (see its doc comment). Baking unsetStyle.Render in here
// used to embed a raw ANSI escape into winRow.text BEFORE renderWindow's
// width-based slicing/wrapping; wrapWidth measures width rune-by-rune with no
// escape-sequence awareness, so a wrap-mode line break could land mid-escape
// and leave a dangling \x1b[38;5;240m on one of the split lines.
func configCell(v string, set bool, width int) string {
	text := v
	if !set {
		text = "(unset)"
	}
	return padRight(truncate(text, width), width)
}

var unsetStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// configRowDecorator dims a row's "(unset)" local/global cells AFTER
// renderWindow has sliced/wrapped/scrolled the (plain) row text, using the
// SAME column widths gitConfigRowText used to lay the cells out — so the
// styled ranges can never drift from the text (the commit_ident.go SYNC
// INVARIANT; mirrors commitLineDecorator's column-span coloring). Returns
// nil when neither scope is unset (nothing to decorate).
//
// Only the row's first visual line (visualLine == 0) is decorated, matching
// dimIdentStyle's wrap handling in commit_ident.go: a wrapped continuation
// segment restarts its rune index at 0, so the absolute column math below
// would misalign against it. Cutoff and scroll modes never produce more than
// one visual line per row, so this only actually skips anything in wrap mode
// on a terminal narrow enough to push the local/global columns past the
// wrap boundary — a cosmetic no-dim, not a correctness bug.
func configRowDecorator(row model.GitConfigRow, keyW, localW, globalW int) rowDecorator {
	localStart := keyW + 1
	globalStart := localStart + localW + 1
	var spans []coloredSpan
	if !row.LocalSet {
		spans = append(spans, coloredSpan{Start: localStart, Length: localW, Style: unsetStyle})
	}
	if !row.GlobalSet {
		spans = append(spans, coloredSpan{Start: globalStart, Length: globalW, Style: unsetStyle})
	}
	if len(spans) == 0 {
		return nil
	}
	return func(visible string, hscroll, visualLine int) string {
		if visualLine != 0 {
			return visible
		}
		r := []rune(visible)
		var b strings.Builder
		i := 0
		for i < len(r) {
			col := i + hscroll
			colored := false
			for _, sp := range spans {
				if col >= sp.Start && col < sp.Start+sp.Length {
					j := i
					for j < len(r) {
						c := j + hscroll
						if c < sp.Start || c >= sp.Start+sp.Length {
							break
						}
						j++
					}
					b.WriteString(sp.Style.Render(string(r[i:j])))
					i = j
					colored = true
					break
				}
			}
			if colored {
				continue
			}
			b.WriteRune(r[i])
			i++
		}
		return b.String()
	}
}
