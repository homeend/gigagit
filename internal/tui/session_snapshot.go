package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// sessionSnapshot is the agent-facing session state contract (schema v1) the
// TUI publishes for `gg mcp`'s gg_ui_state tool. Values are English protocol
// data (hashes, repo-relative paths, engine op names) — never translated
// display strings. Status is the one documented exception: a verbatim copy of
// the visible status line, display-only, explicitly non-parseable.
// See docs/superpowers/specs/2026-07-19-mcp-server-stage1-design.md.
type sessionSnapshot struct {
	Version       int            `json:"version"`
	PID           int            `json:"pid"`
	WrittenAt     string         `json:"written_at,omitempty"` // RFC 3339 UTC; stamped at write time
	Repo          snapRepo       `json:"repo"`
	Focus         snapFocus      `json:"focus"`
	Cursor        snapCursor     `json:"cursor"`
	MarkedCommits []string       `json:"marked_commits,omitempty"`
	MarkedFiles   []string       `json:"marked_files,omitempty"`
	FilesView     *snapFilesView `json:"files_view,omitempty"`
	Switcher      *snapSwitcher  `json:"switcher,omitempty"`
	Filter        *snapFilter    `json:"filter,omitempty"`
	CommitScope   []string       `json:"commit_scope,omitempty"`
	Conflict      *snapConflict  `json:"conflict,omitempty"`
	RunningOp     string         `json:"running_op,omitempty"`
	Status        string         `json:"status,omitempty"`
}

type snapRepo struct {
	CommonDir string `json:"common_dir"`
	Worktree  string `json:"worktree"`
	Branch    string `json:"branch,omitempty"`
	Head      string `json:"head,omitempty"`
}

type snapFocus struct {
	Panel     string `json:"panel"`
	LeftTab   string `json:"left_tab,omitempty"`
	BottomTab string `json:"bottom_tab,omitempty"`
}

type snapCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

type snapCursor struct {
	Commit       *snapCommit `json:"commit,omitempty"`
	Branch       string      `json:"branch,omitempty"`
	RemoteBranch string      `json:"remote_branch,omitempty"`
	Tag          string      `json:"tag,omitempty"`
	Worktree     string      `json:"worktree,omitempty"`
	File         string      `json:"file,omitempty"`
	// Link is the gg:// address of whatever the cursor is on — the same string
	// the . menu's Copy link would put on the clipboard. It is how an agent
	// learns where the user is LOOKING without the user copying anything, and
	// it is protocol data, never display text.
	Link string `json:"link,omitempty"`
}

type snapEndpoint struct {
	Kind    string `json:"kind"` // worktree|index|commit|shelf|ref|pair
	Hash    string `json:"hash,omitempty"`
	ShelfID string `json:"shelf_id,omitempty"`
	// Ref is the branch/tag NAME of a tip endpoint — deliberately the name and
	// not a sha, because a tip is what it addresses and it moves.
	Ref string `json:"ref,omitempty"`
	// PairA and PairB are a change-set endpoint's two resolved shas.
	PairA string `json:"pair_a,omitempty"`
	PairB string `json:"pair_b,omitempty"`
}

type snapFilesView struct {
	Mode         string        `json:"mode"` // changed|full_tree|compare|stash|shelf
	Title        string        `json:"title,omitempty"`
	Commit       string        `json:"commit,omitempty"`
	Left         *snapEndpoint `json:"left,omitempty"`
	Right        *snapEndpoint `json:"right,omitempty"`
	ShelfID      string        `json:"shelf_id,omitempty"`
	SelectedFile string        `json:"selected_file,omitempty"`
	DiffOpen     bool          `json:"diff_open,omitempty"`
}

type snapSwitcher struct {
	Kind       string `json:"kind"` // bookmark|shelf
	SelectedID string `json:"selected_id"`
	Display    string `json:"display,omitempty"`
}

type snapFilter struct {
	Panel     string `json:"panel,omitempty"`
	Query     string `json:"query,omitempty"`
	Highlight string `json:"highlight,omitempty"`
}

type snapConflict struct {
	Op              string   `json:"op"`
	Source          string   `json:"source,omitempty"`
	Target          string   `json:"target,omitempty"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
}

// panelProtoName maps a panel to its stable protocol name.
func panelProtoName(p panel) string {
	switch p {
	case panelBranches:
		return "branches"
	case panelWorktrees:
		return "worktrees"
	case panelRemotes:
		return "remotes"
	case panelFiles:
		return "files"
	case panelStaged:
		return "staged"
	case panelCommits:
		return "commits"
	case panelTags:
		return "tags"
	case panelReflog:
		return "reflog"
	case panelPreviews:
		return "previews"
	case panelPRs:
		return "prs"
	}
	return ""
}

// panelFromProtoName is panelProtoName's inverse: it decodes the protocol name
// a session snapshot writes and a `gg session focus` command carries. Unknown
// names are refused rather than defaulted — an agent that named a panel gg does
// not have should hear so.
func panelFromProtoName(s string) (panel, bool) {
	for _, p := range []panel{panelBranches, panelWorktrees, panelRemotes, panelFiles, panelStaged, panelCommits, panelTags, panelReflog, panelPreviews, panelPRs} {
		if panelProtoName(p) == s {
			return p, true
		}
	}
	return 0, false
}

// filesModeProtoName maps a filesMode to its stable protocol name.
func filesModeProtoName(fm filesMode) string {
	switch fm {
	case filesModeCompare:
		return "compare"
	case filesModeFullTree:
		return "full_tree"
	case filesModeStash:
		return "stash"
	case filesModeShelf:
		return "shelf"
	}
	return "changed"
}

// endpointProto maps a comparison endpoint to its stable protocol form.
//
// ONE ARM PER KIND, no default. The default arm this replaces read "anything
// else is a commit", so a ref or pair endpoint serialized as
// {"kind":"commit","hash":""} — an agent reading the snapshot would be told the
// compare stands on a commit and handed no commit to look at. A kind an agent
// does not recognise is strictly better than a kind that is a lie, so both new
// kinds get a spelling of their own rather than a refusal.
//
// EndpointInvalid panics, in model.Endpoint's own idiom: the zero Endpoint is
// an unset variable, never "the working tree". It is unreachable — this is
// called only in filesModeCompare, and openCompareFiles (files_view.go) now
// DECLINES an endpoint it cannot identify before it stores one, through
// endpointComparable, which refuses EndpointInvalid and EndpointRef by name.
// So the arm exists to keep an impossible state loud rather than let it
// become an empty JSON object nobody can explain.
//
// (An earlier version of this comment credited left.Display(); Display is not
// the first thing that touches the value — compareTagFor/CacheTag() is, and it
// is what the guard was added in front of.)
func endpointProto(e model.Endpoint) *snapEndpoint {
	switch e.Kind() {
	case model.EndpointWorkTree:
		return &snapEndpoint{Kind: "worktree"}
	case model.EndpointIndex:
		return &snapEndpoint{Kind: "index"}
	case model.EndpointShelf:
		return &snapEndpoint{Kind: "shelf", ShelfID: e.ShelfID()}
	case model.EndpointCommit:
		return &snapEndpoint{Kind: "commit", Hash: e.Hash()}
	case model.EndpointRef:
		return &snapEndpoint{Kind: "ref", Ref: e.Ref()}
	case model.EndpointPair:
		return &snapEndpoint{Kind: "pair", PairA: e.PairA(), PairB: e.PairB()}
	default:
		panic(fmt.Sprintf("tui.endpointProto: unusable EndpointKind %d; add a case arm", e.Kind()))
	}
}

// buildSessionSnapshot serializes the agent-relevant slice of the Model. Pure:
// no I/O, no clock (WrittenAt is stamped by maybeWriteSnapshot so the
// write-on-change compare ignores time). Cursor values resolve through the
// same accessors the `.` menus use (backingIndex / selected* / focusedBookmark),
// so what the agent sees is exactly what an action would act on.
func buildSessionSnapshot(m Model) sessionSnapshot {
	s := sessionSnapshot{
		Version: 1,
		PID:     os.Getpid(),
		Repo: snapRepo{
			CommonDir: m.snapshotCommonDir,
			Worktree:  m.snapshotWorktree,
			Branch:    m.status.Branch,
			Head:      m.currentBranchTipHash(),
		},
		Focus: snapFocus{
			Panel:     panelProtoName(m.focus),
			LeftTab:   panelProtoName(m.activeLeftTab),
			BottomTab: panelProtoName(m.activeBottomTab),
		},
		Status: m.statusMsg,
	}
	if bi, ok := m.backingIndex(panelCommits); ok && bi < len(m.commits) {
		c := m.commits[bi]
		s.Cursor.Commit = &snapCommit{Hash: c.Hash, Subject: c.Subject}
	}
	if b, ok := m.selectedBranch(); ok {
		s.Cursor.Branch = b.Name
	}
	if r, ok := m.selectedRemote(); ok {
		s.Cursor.RemoteBranch = r.Name
	}
	if bi, ok := m.backingIndex(panelTags); ok && bi < len(m.tags) {
		s.Cursor.Tag = m.tags[bi].Name
	}
	if w, ok := m.selectedWorktree(); ok {
		s.Cursor.Worktree = w.Path
	}
	for _, c := range m.commits { // feed order; WIP sentinels never match a hash
		if m.commitCompareSet[c.Hash] {
			s.MarkedCommits = append(s.MarkedCommits, c.Hash)
		}
	}
	if len(m.fileMarks) > 0 {
		for p := range m.fileMarks {
			s.MarkedFiles = append(s.MarkedFiles, p)
		}
		slices.Sort(s.MarkedFiles)
	}
	if m.filesView != nil {
		fv := &snapFilesView{
			Mode:     filesModeProtoName(m.filesMode),
			Title:    m.filesTitle,
			Commit:   m.filesHash,
			DiffOpen: m.diffLayer() != nil,
		}
		if m.filesMode == filesModeCompare {
			fv.Left = endpointProto(m.filesLeft)
			fv.Right = endpointProto(m.filesRight)
		}
		if m.filesMode == filesModeShelf {
			fv.ShelfID = m.filesShelfID
		}
		if b, ok := m.focusedBookmark(); ok {
			fv.SelectedFile = b.Path
		}
		s.FilesView = fv
	} else if m.isFilesPanel(m.focus) {
		if b, ok := m.focusedBookmark(); ok {
			s.Cursor.File = b.Path
		}
	}
	if p := layerOf[*bookmarkPopup](m); p != nil {
		if b, ok := p.selected(); ok {
			s.Switcher = &snapSwitcher{Kind: "bookmark", SelectedID: b.ID, Display: b.Address().Display()}
		}
	} else if p := layerOf[*shelfPopup](m); p != nil {
		if e, ok := p.selected(); ok {
			s.Switcher = &snapSwitcher{Kind: "shelf", SelectedID: e.ID, Display: e.Origin.Display()}
		}
	}
	if m.filterQuery != "" || m.highlightQuery != "" {
		s.Filter = &snapFilter{Query: m.filterQuery, Highlight: m.highlightQuery}
		if m.filterQuery != "" {
			s.Filter.Panel = panelProtoName(m.filterPanel)
		}
	}
	if len(m.commitScopeBranches) > 0 {
		s.CommitScope = slices.Clone(m.commitScopeBranches)
	}
	if m.conflict.Op != "" || m.status.Counts().Conflicted > 0 {
		c := &snapConflict{Op: m.conflict.Op, Source: m.conflict.Source, Target: m.conflict.Target}
		for _, f := range m.status.Conflicts() {
			c.ConflictedFiles = append(c.ConflictedFiles, f.Path)
		}
		s.Conflict = c
	}
	if m.running {
		s.RunningOp = m.opName
	}
	// Rides the same write-on-change heartbeat as everything else here (no
	// extra debounce): contextLinkText is pure Model reads.
	if link, ok := m.contextLinkText(); ok {
		s.Cursor.Link = link
	}
	return s
}

// maybeWriteSnapshot serializes the current snapshot and writes it only when
// the payload (timestamp excluded) differs from the last written one. Called
// from the perpetual 1s heartbeat; best-effort — a failed write never
// disturbs the TUI.
func (m Model) maybeWriteSnapshot() Model {
	if m.snapshotPath == "" {
		return m
	}
	snap := buildSessionSnapshot(m)
	data, err := json.Marshal(snap) // WrittenAt empty here — the compare key
	if err != nil || bytes.Equal(data, m.lastSnapshot) {
		return m
	}
	m.lastSnapshot = data
	snap.WrittenAt = time.Now().UTC().Format(time.RFC3339)
	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return m
	}
	writeSnapshotFile(m.snapshotPath, out)
	return m
}

// writeSnapshotFile writes data atomically (temp file + rename), best-effort.
func writeSnapshotFile(path string, data []byte) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "ui-state-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		return
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
	}
}

// removeSnapshotFile deletes the session file (clean exit / repo switch),
// best-effort. The file doubles as session presence.
func removeSnapshotFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// initSnapshotTarget resolves where this session's snapshot lives. This is
// the SYNCHRONOUS startup path: run.go calls it before tea.NewProgram starts,
// off the Bubble Tea event loop, so blocking on two git subprocesses here is
// safe. reRoot (a repo switch mid-session) must NOT call this directly — it
// runs on the Update goroutine, so it disables the snapshot synchronously and
// resolves the new target asynchronously via snapshotTargetCmd instead.
func (m Model) initSnapshotTarget() Model {
	ctx := context.Background()
	m.snapshotCommonDir, m.snapshotPath = "", ""
	if cd, err := m.svc.GitCommonDir(ctx); err == nil && cd != "" {
		m.snapshotCommonDir = cd
		m.snapshotPath = config.SessionSnapshotPath(cd)
	}
	if top, err := m.svc.TopLevel(ctx); err == nil {
		m.snapshotWorktree = top
	}
	m.lastSnapshot = nil
	return m
}

// snapshotTargetMsg carries the resolved snapshot target (common dir +
// worktree) for one svc generation. svc doubles as the staleness guard
// (pointer identity): a repo switch replaces m.svc with a new *domain.Service,
// so a late-arriving resolve from a superseded switch is recognizable and
// dropped by the Update case rather than clobbering the current repo's state.
type snapshotTargetMsg struct {
	svc                 *domain.Service
	commonDir, worktree string
}

// snapshotTargetCmd resolves where svc's session snapshot lives, off the
// Update goroutine (unlike initSnapshotTarget, which is the synchronous
// startup-only path). Used by reRoot after a repo switch: reRoot itself
// disables the snapshot synchronously, and this cmd re-enables it once the
// two git reads land. A GitCommonDir error sends an empty commonDir, which
// keeps the snapshot disabled for this repo (matching initSnapshotTarget's
// own error handling).
func snapshotTargetCmd(svc *domain.Service) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var commonDir, worktree string
		if cd, err := svc.GitCommonDir(ctx); err == nil {
			commonDir = cd
		}
		if top, err := svc.TopLevel(ctx); err == nil {
			worktree = top
		}
		return snapshotTargetMsg{svc: svc, commonDir: commonDir, worktree: worktree}
	}
}
