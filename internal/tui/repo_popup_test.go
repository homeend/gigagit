package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

// seededModel returns a loaded model whose statePath is a temp registry
// containing otherRepo (older) and the model's own repo (newest, via load Touch).
func seededModel(t *testing.T) (Model, string, string) {
	t.Helper()
	_, repo := newRepoDir(t)
	// The "other" entry only needs to exist on disk; a deterministic,
	// non-numeric name makes filter queries collision-proof.
	otherDir := filepath.Join(t.TempDir(), "other-zebra")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, otherDir, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	m := New(domain.New(repo))
	m.statePath = state
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	return m, state, otherDir
}

func TestLoadTouchesRegistry(t *testing.T) {
	t.Parallel()
	m, state, _ := seededModel(t)
	entries := repos.Load(state)
	if len(entries) != 2 {
		t.Fatalf("load should have touched the registry: %+v", entries)
	}
	resolvedWant, _ := filepath.EvalSymlinks(m.currentWorktree)
	resolvedGot, _ := filepath.EvalSymlinks(entries[0].Path)
	if resolvedGot != resolvedWant {
		t.Fatalf("MRU head = %q, want the current repo %q", entries[0].Path, m.currentWorktree)
	}
}

func TestRKeyOpensPopupMRUFirst(t *testing.T) {
	t.Parallel()
	m, _, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	if p == nil {
		t.Fatal("R should open the repo popup")
	}
	if len(p.entries) != 2 {
		t.Fatalf("popup entries = %+v", p.entries)
	}
	resolvedOther, _ := filepath.EvalSymlinks(otherDir)
	resolvedSecond, _ := filepath.EvalSymlinks(p.entries[1].Path)
	if resolvedSecond != resolvedOther {
		t.Fatalf("second entry = %q, want %q", p.entries[1].Path, otherDir)
	}
}

func TestPopupFilterAndSwitch(t *testing.T) {
	t.Parallel()
	m, _, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	// Navigation-first: press / to start filtering, then type.
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	for _, r := range "zebra" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	p := layerOf[*repoPopup](m)
	if !p.filtering {
		t.Fatal("/ should enter filter mode")
	}
	if got := len(p.visible()); got != 1 {
		t.Fatalf("filtered visible = %d, want 1 (query %q)", got, p.query)
	}
	// First enter locks the filter (leaves input mode, popup stays open).
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if p := layerOf[*repoPopup](m); p == nil || p.filtering {
		t.Fatalf("first enter should lock the filter and keep the popup open; p=%v", p)
	}
	// Second enter switches to the single filtered repo.
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if layerOf[*repoPopup](m) != nil {
		t.Fatal("second enter should close the popup")
	}
	resolvedWant, _ := filepath.EvalSymlinks(otherDir)
	resolvedGot, _ := filepath.EvalSymlinks(m.switchTarget)
	if resolvedGot != resolvedWant {
		t.Fatalf("switchTarget = %q, want %q", m.switchTarget, otherDir)
	}
}

func TestEnterOnCurrentRepoIsNoOp(t *testing.T) {
	t.Parallel()
	m, _, _ := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	// Selection starts at 0 = MRU head = the current repo.
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if layerOf[*repoPopup](m) != nil {
		t.Fatal("enter should close the popup")
	}
	if m.switchTarget != "" {
		t.Fatalf("must not re-root into the current repo, switchTarget = %q", m.switchTarget)
	}
}

func TestCtrlDRemovesEntry(t *testing.T) {
	t.Parallel()
	m, state, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("down")) // select the older (other) repo
	m = u.(Model)
	u, _ = m.Update(keyMsg("ctrl+d"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	if len(p.entries) != 1 {
		t.Fatalf("popup should drop the entry, got %+v", p.entries)
	}
	for _, e := range repos.Load(state) {
		resolvedE, _ := filepath.EvalSymlinks(e.Path)
		resolvedOther, _ := filepath.EvalSymlinks(otherDir)
		if resolvedE == resolvedOther {
			t.Fatal("ctrl+d must remove the entry from the state file")
		}
	}
}

func TestPopupEscCancelsAndSwallowsKeys(t *testing.T) {
	t.Parallel()
	m, _, _ := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("p")) // would start SmartPull in normal mode
	m = u.(Model)
	if m.running {
		t.Fatal("popup leaked a global key")
	}
	// Navigation-first: a plain letter is swallowed but does NOT filter (you press
	// / first). This is also what fixes the z-collision (z cycles mode, not query).
	p := layerOf[*repoPopup](m)
	if p.query != "" || p.filtering {
		t.Fatalf("plain key must not filter; query = %q filtering = %v", p.query, p.filtering)
	}
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if layerOf[*repoPopup](m) != nil {
		t.Fatal("esc should close the popup")
	}
}

// TestRepoPopupSlashFilterAndZNotCollision pins the navigation-first contract for
// the repo switcher: / enters filter mode where `z` is a literal query character
// (in nav mode z cycles the display mode), and arrows move the selection while
// typing — the same model as the finder and bookmark/shelf switchers.
func TestRepoPopupSlashFilterAndZNotCollision(t *testing.T) {
	t.Parallel()
	m, _, _ := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	// In nav mode, z cycles the display mode (not a query char).
	p := layerOf[*repoPopup](m)
	mode0 := p.mode
	u, _ = m.Update(keyMsg("ctrl+w"))
	m = u.(Model)
	p = layerOf[*repoPopup](m)
	if p.mode == mode0 || p.query != "" {
		t.Fatalf("nav-mode z should cycle display mode, not type a query; mode==%v query=%q", p.mode, p.query)
	}
	// / then a z-containing query types literally.
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	for _, r := range "zeb" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	p = layerOf[*repoPopup](m)
	if p.query != "zeb" {
		t.Fatalf("/zeb should type literally in filter mode; query=%q", p.query)
	}
}

func TestPopupRendersAndFits(t *testing.T) {
	t.Parallel()
	m, _, _ := seededModel(t)
	m.width, m.height = 80, 24
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	out := m.View()
	if !strings.Contains(out, "Switch repository") {
		t.Fatalf("popup title missing:\n%s", out)
	}
	for i, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols, want <= %d", i, w, m.width)
		}
	}
}

func TestAgeString(t *testing.T) {
	t.Parallel()
	now := time.Unix(100000, 0)
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-49 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := ageString(now, c.t); got != c.want {
			t.Errorf("ageString(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestRepoPopupDoesNotWrapLongPath(t *testing.T) {
	t.Parallel()
	m := Model{width: 80, height: 24}
	long := "/very/deeply/nested/path/that/is/way/longer/than/the/popup/box/myrepo"
	m = m.pushLayer(&repoPopup{
		entries: []repos.Entry{{Path: long, LastOpened: time.Now()}},
		now:     time.Now(),
	})
	p := layerOf[*repoPopup](m)
	out := p.box(m)
	// No line may exceed the terminal width.
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("popup line exceeds width (%d): %q", w, line)
		}
	}
	// The long path must occupy exactly ONE line: in cutoff mode it is elided
	// from the LEFT (the leaf is what distinguishes repos) so the age column
	// survives on the same line.
	pathLines := 0
	for _, line := range strings.Split(out, "\n") {
		c := ansiStrip(line)
		if !strings.Contains(c, "myrepo  ") {
			continue
		}
		pathLines++
		if !strings.Contains(c, "…") {
			t.Errorf("long path should be left-elided with …: %q", c)
		}
		if !strings.Contains(c, "(just now)") {
			t.Errorf("age must survive a long path in cutoff mode: %q", c)
		}
	}
	if pathLines != 1 {
		t.Errorf("path rendered across %d lines, want 1 (no wrap):\n%s", pathLines, out)
	}
}

// TestRepoPopupWrapModeIndentsContinuations pins the z-wrap layout fix: wrap
// continuations hang-indent under the entry name (past the "> ● " column), so
// multi-line entries stay readable.
func TestRepoPopupWrapModeIndentsContinuations(t *testing.T) {
	t.Parallel()
	m := Model{width: 60, height: 30}
	long1 := "/very/deeply/nested/path/that/is/longer/than/the/box/one-tail"
	long2 := "/very/deeply/nested/path/that/is/longer/than/the/box/two-tail"
	m = m.pushLayer(&repoPopup{
		entries: []repos.Entry{{Path: long1, LastOpened: time.Now()}, {Path: long2, LastOpened: time.Now()}},
		now:     time.Now(),
		mode:    modeWrap,
	})
	p := layerOf[*repoPopup](m)
	lines := strings.Split(p.box(m), "\n")
	inner := func(s string) string { // content between the border glyphs
		r := []rune(ansiStrip(s))
		if len(r) < 2 {
			return ""
		}
		return string(r[1 : len(r)-1])
	}
	lead := func(s string) int {
		n := 0
		for _, r := range s {
			if r != ' ' {
				break
			}
			n++
		}
		return n
	}
	first, second := -1, -1
	for i, l := range lines {
		c := inner(l)
		if first == -1 && strings.Contains(c, "> ") {
			first = i
		}
		if second == -1 && strings.Contains(c, "two-tail") {
			second = i
		}
	}
	if first == -1 || second == -1 || second <= first+1 {
		t.Fatalf("could not locate both wrapped entries (first=%d second=%d):\n%s", first, second, strings.Join(lines, "\n"))
	}
	// The continuation line hangs 4 columns (the "> ● " prefix) past the
	// first line's start.
	if got, want := lead(inner(lines[first+1])), lead(inner(lines[first]))+4; got != want {
		t.Errorf("continuation indent = %d, want %d:\n%s", got, want, strings.Join(lines, "\n"))
	}
	// No blank separator lines between entries (indent-only layout — the
	// separator variant was tried and rejected).
	for i := first + 1; i < second; i++ {
		if strings.TrimSpace(inner(lines[i])) == "" {
			t.Errorf("unexpected blank line %d between wrapped entries:\n%s", i, strings.Join(lines, "\n"))
		}
	}
}

// TestRepoPopupOpensFullscreen pins the always-fullscreen switcher: the box
// renders at the maximized width and row budget from the start, and ctrl+t is
// inert (the popup no longer implements maximizableLayer).
func TestRepoPopupOpensFullscreen(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.width, m.height = 200, 50
	p := &repoPopup{now: time.Now()}
	for i := 0; i < 30; i++ { // more than the old fixed cap of 12
		p.entries = append(p.entries, repos.Entry{Path: fmt.Sprintf("/home/user/repos/project-number-%d", i), LastOpened: time.Now()})
	}
	m = m.pushLayer(p)

	box := p.box(m)
	if got, want := lipgloss.Width(box), popupFullInnerWidth(m.width)+2; got != want {
		t.Fatalf("box width = %d, want fullscreen %d", got, want)
	}
	rows := 0
	for _, line := range strings.Split(box, "\n") {
		if strings.Contains(ansiStrip(line), "project-number-") {
			rows++
		}
	}
	if rows <= 12 {
		t.Fatalf("fullscreen popup shows %d rows, want more than the old cap of 12", rows)
	}

	// ctrl+t must be inert: the central dispatch only toggles layers that
	// implement maximizableLayer, and the popup's own update swallows the key.
	if _, ok := layer(p).(maximizableLayer); ok {
		t.Fatal("repoPopup must not implement maximizableLayer anymore")
	}
	m, _ = p.update(m, keyMsg("ctrl+t"))
	if after := p.box(m); after != box {
		t.Fatal("ctrl+t must be inert on the always-fullscreen switcher")
	}
}

// TestRepoPopupTableAlignsColumns pins the table layout: across rows with
// different name lengths, the name, slow-fs, path, and age fields each start
// at one shared column.
func TestRepoPopupTableAlignsColumns(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m := Model{width: 120, height: 40}
	long := "/tmp/repos/a-much-longer-repository-name"
	p := &repoPopup{
		entries: []repos.Entry{
			{Path: "/tmp/repos/alpha", LastOpened: now},
			{Path: long, LastOpened: now},
		},
		now:     now,
		foreign: map[string]bool{long: true},
	}
	m = m.pushLayer(p)

	var dataRows []string
	for _, line := range strings.Split(p.box(m), "\n") {
		if c := ansiStrip(line); strings.Contains(c, "(just now)") {
			dataRows = append(dataRows, c)
		}
	}
	if len(dataRows) != 2 {
		t.Fatalf("want 2 data rows, got %d:\n%s", len(dataRows), strings.Join(dataRows, "\n"))
	}
	pathCol := strings.Index(dataRows[0], "/tmp")
	ageCol := strings.Index(dataRows[0], "(just now)")
	for _, r := range dataRows[1:] {
		if got := strings.Index(r, "/tmp"); got != pathCol {
			t.Errorf("path column = %d, want %d:\n%s", got, pathCol, strings.Join(dataRows, "\n"))
		}
		if got := strings.Index(r, "(just now)"); got != ageCol {
			t.Errorf("age column = %d, want %d:\n%s", got, ageCol, strings.Join(dataRows, "\n"))
		}
	}
	// The slow-fs marker sits in its own column, ending right before the path
	// column's gap (it never rides directly after the name).
	slowRow := dataRows[1]
	slowCol := strings.Index(slowRow, "(slow fs)")
	if slowCol < 0 {
		t.Fatalf("foreign row lost its (slow fs) marker: %q", slowRow)
	}
	if end := slowCol + lipgloss.Width("(slow fs)"); end+2 != pathCol {
		t.Errorf("(slow fs) ends at %d, want the path column at %d to start 2 after it", end, pathCol)
	}
}

// TestRepoPopupSlowColumnAlwaysReserved pins that the async probe verdicts
// landing must not shift the path column: the slow-fs column is reserved even
// while foreign is nil.
func TestRepoPopupSlowColumnAlwaysReserved(t *testing.T) {
	t.Parallel()
	now := time.Now()
	entries := []repos.Entry{{Path: "/tmp/repos/alpha", LastOpened: now}}
	m := Model{width: 120, height: 40}
	pathColOf := func(p *repoPopup) int {
		mm := m.pushLayer(p)
		for _, line := range strings.Split(p.box(mm), "\n") {
			if c := ansiStrip(line); strings.Contains(c, "(just now)") {
				return strings.Index(c, "/tmp")
			}
		}
		return -1
	}
	before := pathColOf(&repoPopup{entries: entries, now: now})
	after := pathColOf(&repoPopup{entries: entries, now: now, foreign: map[string]bool{entries[0].Path: true}})
	if before < 0 || before != after {
		t.Fatalf("path column moved when probe verdicts landed: before=%d after=%d", before, after)
	}
}

// TestRepoPopupColumnsStableWhileFiltering pins that column widths derive from
// ALL entries, so narrowing the filtered view does not re-flow the table.
func TestRepoPopupColumnsStableWhileFiltering(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m := Model{width: 120, height: 40}
	p := &repoPopup{
		entries: []repos.Entry{
			{Path: "/tmp/repos/alpha", LastOpened: now},
			{Path: "/tmp/repos/a-much-longer-repository-name", LastOpened: now},
		},
		now: now,
	}
	m = m.pushLayer(p)
	pathCol := func() int {
		for _, line := range strings.Split(p.box(m), "\n") {
			if c := ansiStrip(line); strings.Contains(c, "(just now)") {
				return strings.Index(c, "/tmp")
			}
		}
		return -1
	}
	all := pathCol()
	p.query = "alpha" // filters down to the short-named entry only
	if filtered := pathCol(); all < 0 || filtered != all {
		t.Fatalf("path column re-flowed under filter: all=%d filtered=%d", all, filtered)
	}
}

func TestRepoPopupTKeyIsLiteralWhileFiltering(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.width, m.height = 200, 50
	p := &repoPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.query != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; query=%q`, p.query)
	}
}
