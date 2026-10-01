package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/repos"
)

// groupFixture is an MRU-ordered registry: two gigagit checkouts split by a
// lazygit one, plus a pair each of the two "no usable remote" shapes (unknown
// and the NoRemote sentinel), which must never be lumped into a group.
func groupFixture(now time.Time) []repos.Entry {
	at := func(min int) time.Time { return now.Add(-time.Duration(min) * time.Minute) }
	return []repos.Entry{
		{Path: "/r/wt/recycle", Remote: "gigagit", LastOpened: at(1)},
		{Path: "/r/lazy-dir", Remote: "lazygit", LastOpened: at(2)},
		{Path: "/r/unknown-a", Remote: "", LastOpened: at(3)},
		{Path: "/r/gigagit", Remote: "gigagit", LastOpened: at(4)},
		{Path: "/r/unknown-b", Remote: "", LastOpened: at(5)},
		{Path: "/r/bare-a", Remote: repos.NoRemote, LastOpened: at(6)},
		{Path: "/r/bare-b", Remote: repos.NoRemote, LastOpened: at(7)},
	}
}

func visiblePaths(p *repoPopup) []string {
	var out []string
	for _, e := range p.visible() {
		out = append(out, e.Path)
	}
	return out
}

// dataRows returns the popup's entry rows (the ones carrying an age).
func dataRows(p *repoPopup, m Model) []string {
	var rows []string
	for _, line := range strings.Split(p.box(m), "\n") {
		if c := ansiStrip(line); strings.Contains(c, " ago)") {
			rows = append(rows, c)
		}
	}
	return rows
}

// nameCell is a rendered row's name column: everything left of the path, minus
// the box border and the cursor/current markers.
func nameCell(row string) string {
	return strings.Trim(row[:strings.Index(row, "/r/")], "║>● ")
}

func TestRepoPopupGroupedOrder(t *testing.T) {
	t.Parallel()
	now := time.Now()
	p := &repoPopup{entries: groupFixture(now), now: now, grouped: true}
	want := []string{
		"/r/wt/recycle", "/r/gigagit", // the group of the most recent entry leads
		"/r/lazy-dir",
		"/r/unknown-a", "/r/unknown-b", // MRU order; NOT a group
		"/r/bare-a", "/r/bare-b",
	}
	if got := visiblePaths(p); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("grouped order =\n%v\nwant\n%v", got, want)
	}
	p.grouped = false
	if got := visiblePaths(p); got[1] != "/r/lazy-dir" || got[3] != "/r/gigagit" {
		t.Fatalf("flat mode must keep plain MRU order, got %v", got)
	}
}

func TestRepoPopupGroupedRowsNameOnlyTheHead(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m := Model{width: 120, height: 40}
	p := &repoPopup{entries: groupFixture(now), now: now, grouped: true}
	m = m.pushLayer(p)

	rows := dataRows(p, m)
	if len(rows) != 7 {
		t.Fatalf("want 7 rows, got %d:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(p.box(m), "[grouped]") {
		t.Error("the header must say the list is grouped")
	}
	// The head shows the PROJECT name (the remote repository), not the
	// worktree directory's own name.
	pathCol := strings.Index(rows[0], "/r/")
	if name := nameCell(rows[0]); name != "gigagit" {
		t.Errorf("head name = %q, want gigagit:\n%s", name, rows[0])
	}
	// The member under it has a blank name column, its path in the same column.
	if got := strings.Index(rows[1], "/r/"); got != pathCol {
		t.Errorf("member path column = %d, want %d:\n%s", got, pathCol, strings.Join(rows, "\n"))
	}
	if name := nameCell(rows[1]); name != "" {
		t.Errorf("member row must not repeat the name, got %q", name)
	}
	// A project with a single row is no group: it keeps its directory name, as
	// in the flat list, and so does every entry without a usable remote.
	for i, want := range map[int]string{2: "lazy-dir", 3: "unknown-a", 4: "unknown-b", 5: "bare-a", 6: "bare-b"} {
		if name := nameCell(rows[i]); name != want {
			t.Errorf("row %d name = %q, want %q", i, name, want)
		}
	}
}

func TestRepoPopupGroupedFilterNamesWhatIsLeft(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m := Model{width: 120, height: 40}
	entries := append(groupFixture(now), repos.Entry{Path: "/r/wt/other", Remote: "gigagit", LastOpened: now.Add(-time.Hour)})
	// "/r/wt/" drops the group's /r/gigagit row: the two survivors still form a
	// group, and the first of them carries the project name.
	p := &repoPopup{entries: entries, now: now, grouped: true, query: "/r/wt/"}
	m = m.pushLayer(p)
	rows := dataRows(p, m)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if nameCell(rows[0]) != "gigagit" || nameCell(rows[1]) != "" {
		t.Errorf("the surviving rows must read as one named group:\n%s", strings.Join(rows, "\n"))
	}
	// Narrowed to one row, it is a group no longer and shows its own name.
	p.query = "recycle"
	rows = dataRows(p, m)
	if len(rows) != 1 || nameCell(rows[0]) != "recycle" {
		t.Errorf("a lone surviving row must show its directory name:\n%s", strings.Join(rows, "\n"))
	}
}

func TestRepoPopupCtrlGTogglesAndIsRemembered(t *testing.T) {
	t.Parallel()
	m, state, _ := seededModel(t)
	m = tempPromptStore(t, m)
	now := time.Now()
	for _, e := range []struct{ dir, remote string }{{"proj-wt", "proj"}, {"filler", "filler"}, {"proj-main", "proj"}} {
		dir := t.TempDir() + "/" + e.dir
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		now = now.Add(-time.Hour)
		if err := repos.Touch(state, dir, e.remote, now); err != nil {
			t.Fatal(err)
		}
	}
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	if p.grouped {
		t.Fatal("the flat list is the default")
	}
	// Park the cursor on proj-main (flat index 3); grouping moves that row up.
	for i, e := range p.visible() {
		if strings.HasSuffix(e.Path, "proj-main") {
			p.sel = i
		}
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = u.(Model)
	p = layerOf[*repoPopup](m)
	if !p.grouped {
		t.Fatal("ctrl+g should group the list")
	}
	vis := p.visible()
	if !strings.HasSuffix(vis[p.sel].Path, "proj-main") {
		t.Fatalf("the cursor must stay on its row across the toggle, now on %q", vis[p.sel].Path)
	}
	if !strings.HasSuffix(vis[p.sel-1].Path, "proj-wt") {
		t.Fatalf("proj-main should sit right under proj-wt, got %v", visiblePaths(p))
	}
	if p.query != "" {
		t.Fatalf("ctrl+g must not type into the filter, query=%q", p.query)
	}
	// Close and reopen: the choice is remembered for the session.
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("R"))
	m = u.(Model)
	if p = layerOf[*repoPopup](m); p == nil || !p.grouped {
		t.Fatal("grouped mode should survive closing and reopening the switcher")
	}
	// It is remembered across sessions too: a new TUI over the same
	// machine-local store opens the switcher grouped.
	if !m.promptStore.RepoGrouped() {
		t.Fatal("ctrl+g must persist the grouping")
	}
	next := New(m.svc)
	next.promptStore = m.promptStore
	next = next.loadPrefs()
	if !next.repoGrouped {
		t.Fatal("a new session must start grouped")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = u.(Model)
	if layerOf[*repoPopup](m).grouped {
		t.Fatal("a second ctrl+g should return to the flat list")
	}
	if m.promptStore.RepoGrouped() {
		t.Fatal("the flat choice must persist too")
	}
}

func TestRepoPopupCtrlPCopiesTheAbsolutePath(t *testing.T) {
	t.Parallel()
	m, _, otherDir := seededModel(t)
	m.width, m.height = 120, 40
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("down")) // the older entry: other-zebra
	m = u.(Model)
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = u.(Model)
	if cmd == nil {
		t.Fatal("ctrl+p should return the copy command")
	}
	u, _ = m.Update(cmd())
	m = u.(Model)
	if copied != otherDir {
		t.Fatalf("copied %q, want the selected row's absolute path %q", copied, otherDir)
	}
	p := layerOf[*repoPopup](m)
	if p == nil {
		t.Fatal("copying must leave the switcher open")
	}
	if p.query != "" {
		t.Fatalf("ctrl+p must not type into the filter, query=%q", p.query)
	}
	if !strings.Contains(ansiStrip(m.View()), "Copied absolute path") {
		t.Errorf("the copy confirmation must be visible while the switcher is open:\n%s", ansiStrip(m.View()))
	}
}

func TestRepoPopupHintsAdvertiseGroupAndCopy(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m := Model{width: 120, height: 40}
	p := &repoPopup{entries: groupFixture(now), now: now}
	m = m.pushLayer(p)
	box := ansiStrip(p.box(m))
	for _, want := range []string{"[ctrl+g] group", "[ctrl+p] copy path"} {
		if !strings.Contains(box, want) {
			t.Errorf("hint line is missing %q", want)
		}
	}
}

// commonFixture: two checkouts of a repository with NO remote (the main one and
// a linked worktree), split by an unrelated entry, plus a third checkout that
// shares the common dir of a remote-named clone group.
func commonFixture(now time.Time) ([]repos.Entry, map[string]string) {
	at := func(min int) time.Time { return now.Add(-time.Duration(min) * time.Minute) }
	entries := []repos.Entry{
		{Path: "/r/test-1", Remote: repos.NoRemote, LastOpened: at(1)},
		{Path: "/r/other", Remote: repos.NoRemote, LastOpened: at(2)},
		{Path: "/r/test-1.worktrees/a", Remote: "", LastOpened: at(3)},
		{Path: "/r/clone", Remote: "proj", LastOpened: at(4)},
		{Path: "/r/clone-wt", Remote: "", LastOpened: at(5)},
		{Path: "/r/clone2", Remote: "proj", LastOpened: at(6)},
	}
	common := map[string]string{
		"/r/test-1":             "/r/test-1/.git",
		"/r/other":              "/r/other/.git",
		"/r/test-1.worktrees/a": "/r/test-1/.git",
		"/r/clone":              "/r/clone/.git",
		"/r/clone-wt":           "/r/clone/.git",
		"/r/clone2":             "/r/clone2/.git",
	}
	return entries, common
}

// A checkout and its linked worktrees group by their shared git common dir
// even with no remote; the group is named after the main checkout's directory.
// A remote name still bridges separate clones, and a worktree with no recorded
// remote joins its clone's group through the common dir.
func TestRepoPopupGroupsByCommonDir(t *testing.T) {
	t.Parallel()
	now := time.Now()
	entries, common := commonFixture(now)
	m := Model{width: 120, height: 40}
	p := &repoPopup{entries: entries, now: now, grouped: true, common: common}
	m = m.pushLayer(p)
	want := []string{
		"/r/test-1", "/r/test-1.worktrees/a",
		"/r/other",
		"/r/clone", "/r/clone-wt", "/r/clone2",
	}
	if got := visiblePaths(p); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("grouped order =\n%v\nwant\n%v", got, want)
	}
	rows := dataRows(p, m)
	for i, want := range []string{"test-1", "", "other", "proj", "", ""} {
		if name := nameCell(rows[i]); name != want {
			t.Errorf("row %d name = %q, want %q\n%s", i, name, want, strings.Join(rows, "\n"))
		}
	}
}

// The probe's common dirs land after the popup opened: the regrouping must
// keep the cursor on the row it was on.
func TestRepoPopupFSMsgRegroupKeepsTheCursor(t *testing.T) {
	t.Parallel()
	now := time.Now()
	entries, common := commonFixture(now)
	m := Model{width: 120, height: 40}
	p := &repoPopup{entries: entries, now: now, grouped: true}
	m = m.pushLayer(p)
	for i, e := range p.visible() {
		if e.Path == "/r/test-1.worktrees/a" {
			p.sel = i
		}
	}
	u, _ := m.Update(repoFSMsg{foreign: map[string]bool{}, common: common})
	m = u.(Model)
	p = layerOf[*repoPopup](m)
	if got := p.visible()[p.sel].Path; got != "/r/test-1.worktrees/a" {
		t.Fatalf("cursor moved to %q when the common dirs landed", got)
	}
	if p.sel != 1 {
		t.Fatalf("the worktree should now sit under its checkout, sel=%d %v", p.sel, visiblePaths(p))
	}
}

// End to end over a real repository: the probe reads a linked worktree's
// common dir, and the switcher then groups it with its checkout.
func TestRepoPopupProbeGroupsARealWorktree(t *testing.T) {
	t.Parallel()
	m, state, _ := seededModel(t)
	wt := filepath.Join(t.TempDir(), "linked")
	gitRun(t, m.currentWorktree, "worktree", "add", "-q", "-b", "side", wt)
	if err := repos.Touch(state, wt, repos.NoRemote, time.Unix(500, 0)); err != nil {
		t.Fatal(err)
	}
	mm, cmd, ok := m.openRepoPopup()
	if !ok {
		t.Fatal("openRepoPopup refused")
	}
	u, _ := mm.Update(cmd())
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	p.grouped = true
	vis := p.visible()
	if len(vis) != 3 || !samePathTUI(vis[1].Path, wt) {
		t.Fatalf("the linked worktree should sit under its checkout: %v", visiblePaths(p))
	}
}
