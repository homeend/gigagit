package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/textdiff"
)

func TestLineAnchorFindsBothSidesAndFolds(t *testing.T) {
	t.Parallel()
	v := &diffView{lines: wrapLines([]textdiff.Line{
		{Row: textdiff.Row{LeftNo: 1, RightNo: 1}},
		{Fold: 8}, // hides old 2-9 / new 2-9
		{Row: textdiff.Row{LeftNo: 10, RightNo: 10}},
		{Row: textdiff.Row{LeftNo: 0, RightNo: 11}}, // an added line: no old number
	})}
	if li, vis := v.lineAnchor(10, false); li != 2 || !vis {
		t.Errorf("new:10 = (%d,%v), want (2,true)", li, vis)
	}
	if li, vis := v.lineAnchor(10, true); li != 2 || !vis {
		t.Errorf("old:10 = (%d,%v), want (2,true)", li, vis)
	}
	if li, vis := v.lineAnchor(5, false); li != 1 || vis {
		t.Errorf("a folded number must return its FOLD entry, got (%d,%v), want (1,false)", li, vis)
	}
	if li, _ := v.lineAnchor(99, false); li != -1 {
		t.Errorf("a number past the end = %d, want -1", li)
	}
	if li, _ := v.lineAnchor(11, true); li != -1 {
		t.Errorf("new-only line 11 must not resolve on the old side, got %d", li)
	}
	if got := v.lastLineNo(false); got != 11 {
		t.Errorf("lastLineNo(new) = %d, want 11", got)
	}
	if got := v.lastLineNo(true); got != 10 {
		t.Errorf("lastLineNo(old) = %d, want 10", got)
	}
}

func TestPanelFromProtoNameRoundTrips(t *testing.T) {
	t.Parallel()
	for _, p := range []panel{panelBranches, panelWorktrees, panelRemotes, panelFiles, panelStaged, panelCommits, panelTags, panelReflog, panelPreviews} {
		name := panelProtoName(p)
		if name == "" {
			t.Fatalf("panel %v has no protocol name", p)
		}
		got, ok := panelFromProtoName(name)
		if !ok || got != p {
			t.Errorf("panelFromProtoName(%q) = (%v,%v), want (%v,true)", name, got, ok, p)
		}
	}
	if _, ok := panelFromProtoName("nowhere"); ok {
		t.Error("an unknown panel name must be refused")
	}
	if _, ok := panelFromProtoName(""); ok {
		t.Error("an empty panel name must be refused")
	}
}

func TestSteerFocusSwitchesPanelsAndAnswers(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m = m.pushLayer(&diffView{}) // popped to reach the panels
	c := steer.Command{ID: "f-1", Cmd: "focus", Panel: "branches", Wait: true}
	m, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	if m.focus != panelBranches {
		t.Errorf("focus = %v, want panelBranches", m.focus)
	}
	if m.topLayer() != nil {
		t.Error("focus must pop the layer stack to reach the panels")
	}
	r, ok := steer.AwaitReply(dir, "f-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}
}

// Every "an agent did this" notice wears the ▸ prefix — they all land in the
// same two fields through the same steerNotice, so one without it reads as a
// different kind of message.
func TestSteerNoticesAllCarryTheAgentPrefix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cmd  steer.Command
	}{
		{"focus", steer.Command{ID: "px-1", Cmd: "focus", Panel: "branches"}},
		{"reload", steer.Command{ID: "px-2", Cmd: "reload", Sources: []string{"notes"}}},
		{"highlight", markCmd("px-3", "info", 1, 2)},
		{"highlight_clear", steer.Command{ID: "px-4", Cmd: "highlight_clear"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Its OWN model, not a shared base: Model is a value, but the
			// registry maps (srcGen/srcInflight/srcLoading) and `attention`
			// are maps shared by every copy, and these verbs write them —
			// parallel subtests over one base would be a data race.
			m, _ := steerModel(t)
			m = m.initSteerInbox()
			m.ready = true
			m, cmd := m.applySteer(tc.cmd)
			runSteerCmd(t, cmd)
			if !strings.HasPrefix(m.statusMsg, "▸ ") {
				t.Errorf("%s notice = %q, want the ▸ agent prefix", tc.name, m.statusMsg)
			}
		})
	}
}

func TestSteerFocusRefusesAnUnknownPanel(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, cmd := m.applySteer(steer.Command{ID: "f-2", Cmd: "focus", Panel: "nowhere", Wait: true})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "f-2", time.Second)
	if r.OK || !strings.Contains(r.Error, "nowhere") {
		t.Fatalf("reply = %+v, want ok:false naming the panel", r)
	}
	_ = m
}

func TestSteerStepWithoutAnOpenDiffIsRefused(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, cmd := m.applySteer(steer.Command{ID: "s-1", Cmd: "navigate", Step: "next_note", Wait: true})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "s-1", time.Second)
	if r.OK {
		t.Fatalf("reply = %+v, want ok:false with no diff open", r)
	}
	_ = m
}

// navRepo is a real repo with one committed 40-line file and one unstaged edit
// on line 18, so the Files panel carries exactly one diffable row and new line
// 18 is a real landing target. It returns the repo's path.
//
// NOTE the suite's two shapes: `newRepo(t)` (load_test.go:18) returns a
// *git.Repo for domain.New; this helper returns a PATH for domain.Open, which
// is the same service either way. `gitRun(t, dir, args...)` is load_test.go:87.
func navRepo(t *testing.T) string {
	t.Helper()
	dir := gittest.BasicRepo(t, "hi\n")
	lines := make([]string, 0, 40)
	for i := 1; i <= 40; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-m", "seed")
	lines[17] = "EDITED" // new line 18
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSteerNavigateLandsTheCursorOnANewSideLine(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	c := steer.Command{
		ID: "n-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 18},
		Wait:   true,
	}
	m, cmd := m.applySteer(c)
	if m.pendingSteer == nil {
		t.Fatal("navigate must park a pendingSteer until the diff loads")
	}
	m = pumpDiff(t, m, cmd)
	v := m.diffLayer()
	if v == nil {
		t.Fatal("no diff view after navigate")
	}
	row, ok := v.cursorRow()
	if !ok {
		t.Fatal("no cursor row after the landing")
	}
	if row.RightNo != 18 {
		t.Errorf("cursor landed on new line %d, want 18", row.RightNo)
	}
	if m.pendingSteer != nil {
		t.Error("the pending steer must be cleared once it lands")
	}
	r, ok := steer.AwaitReply(dir, "n-1", 2*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "a.txt:18") {
		t.Fatalf("reply = %+v ok=%v, want ok:true detailing a.txt:18", r, ok)
	}
}

// TestSteerNavigateExpandsAFoldToReachTheLine is the partial-view case: with
// the f-toggle collapsed view open, everything but 15-21 hides under a fold, so
// line 5 resolves to a FOLD entry. The landing must expand it (what the note
// jump does), not read the fold hit as "not in this diff".
func TestSteerNavigateExpandsAFoldToReachTheLine(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.diffPartial = true
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-6", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 5},
		Wait:   true,
	})
	m = pumpDiff(t, m, cmd)
	v := m.diffLayer()
	if v == nil {
		t.Fatal("no diff view after navigate")
	}
	row, ok := v.cursorRow()
	if !ok {
		t.Fatal("no cursor row after the landing")
	}
	if row.RightNo != 5 {
		t.Errorf("cursor landed on new line %d, want 5 (the fold must be expanded)", row.RightNo)
	}
	// Fixture guard: expandFoldFor is the ONLY thing that clears diffPartial,
	// so a still-true flag would mean line 5 was never folded and the test
	// proved nothing.
	if m.diffPartial {
		t.Error("the collapsed view never folded line 5 away — this test is not exercising the fold path")
	}
	r, ok := steer.AwaitReply(dir, "n-6", 2*time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}
	if strings.Contains(r.Detail, "clamped") {
		t.Errorf("detail = %q — line 5 exists, nothing was clamped", r.Detail)
	}
}

// TestSteerNavigateClampsToTheFilesLastLineNotTheLastVisibleOne pins the other
// half of the fold rule: in a collapsed view the tail is folded away, so a
// too-large number must clamp to the FILE's last line (40) after expanding,
// never to the last line that happened to be visible (21).
func TestSteerNavigateClampsToTheFilesLastLineNotTheLastVisibleOne(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.diffPartial = true
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-7", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 9999},
		Wait:   true,
	})
	m = pumpDiff(t, m, cmd)
	v := m.diffLayer()
	if v == nil {
		t.Fatal("no diff view after navigate")
	}
	row, ok := v.cursorRow()
	if !ok {
		t.Fatal("no cursor row after the landing")
	}
	if row.RightNo != 40 {
		t.Errorf("cursor landed on new line %d, want 40 (the file's last line, not the last VISIBLE one)", row.RightNo)
	}
	if m.diffPartial { // fixture guard: the tail really was folded away
		t.Error("the collapsed view never folded the tail — this test is not exercising the fold path")
	}
	r, ok := steer.AwaitReply(dir, "n-7", 2*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "clamped to line 40") {
		t.Fatalf("reply = %+v ok=%v, want ok:true clamped to line 40", r, ok)
	}
}

func TestSteerNavigateClampsPastEndOfFile(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-2", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 9999},
		Wait:   true,
	})
	m = pumpDiff(t, m, cmd)
	r, ok := steer.AwaitReply(dir, "n-2", 2*time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true — a too-large number clamps", r, ok)
	}
	if !strings.Contains(r.Detail, "clamped to line") {
		t.Errorf("detail = %q, want it to say the landing was clamped", r.Detail)
	}
	_ = m
}

func TestSteerNavigateRefusesAPathNotInTheDiff(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-3", Cmd: "navigate", File: "nope.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 1},
		Wait:   true,
	})
	// The miss triggers ONE status reload and a retry before it gives up.
	m = pumpAll(t, m, cmd)
	r, ok := steer.AwaitReply(dir, "n-3", 3*time.Second)
	if !ok || r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:false", r, ok)
	}
	if !strings.Contains(r.Error, "nope.txt") {
		t.Errorf("error = %q, want it to name the path", r.Error)
	}
	_ = m
}

func TestSteerNavigateRefusesAConflictedRow(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	// Force the row to read as conflicted without building a real merge: the
	// refusal is a property of the row's kind, not of how it got that way.
	for i := range m.status.Files {
		if m.status.Files[i].Path == "a.txt" {
			m.status.Files[i].Kind = model.KindUnmerged
		}
	}
	m, cmd := m.applySteer(steer.Command{
		ID: "n-4", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 1},
		Wait:   true,
	})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "n-4", time.Second)
	if r.OK || !strings.Contains(r.Error, "conflict") {
		t.Fatalf("reply = %+v, want ok:false mentioning the conflict editor", r)
	}
	_ = m
}

func TestSteerNavigateRefusesAnUnloadedCommit(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-5", Cmd: "navigate", Commit: "0123456789abcdef0123456789abcdef01234567", Wait: true,
	})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "n-5", time.Second)
	if r.OK || !strings.Contains(r.Error, "not loaded") {
		t.Fatalf("reply = %+v, want ok:false \"commit not loaded in the feed\" — an agent must never raise the eager-search prompt", r)
	}
	_ = m
}

// TestSteerNavigateRefusalLeavesTheUsersViewAlone pins the "never throw away
// what the user is in the middle of" half of a refusal: a navigate that cannot
// be served must not first have popped the layer the user was reading.
func TestSteerNavigateRefusalLeavesTheUsersViewAlone(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m = m.pushLayer(&diffView{title: "user's own diff"})
	m, cmd := m.applySteer(steer.Command{
		ID: "n-8", Cmd: "navigate", Commit: "0123456789abcdef0123456789abcdef01234567", Wait: true,
	})
	runSteerCmd(t, cmd)
	if m.topLayer() == nil {
		t.Error("a refused navigate must leave the user's open view on the stack")
	}
}

func TestSteerNavigateRefusesASecondNavigateWhileOneIsLoading(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, _ = m.applySteer(steer.Command{
		ID: "n-9", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 18},
		Wait:   true,
	})
	if m.pendingSteer == nil {
		t.Fatal("the first navigate must park")
	}
	m, cmd := m.applySteer(steer.Command{
		ID: "n-10", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 2},
		Wait:   true,
	})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "n-10", time.Second)
	if r.OK || !strings.Contains(r.Error, "still loading") {
		t.Fatalf("reply = %+v, want ok:false — a second navigate must not park over the first", r)
	}
	if m.pendingSteer == nil || m.pendingSteer.cmd.ID != "n-9" {
		t.Errorf("pending = %+v, want the first navigate untouched", m.pendingSteer)
	}
}

// TestSteerNavigateOpensAFileInALoadedCommit walks the longest chain in the
// verb: goto the feed row → openChangedFiles → commitFilesMsg →
// drainPendingFiles → openDiffForFileLine → diffMsg → drainPendingDiff. Every
// gate on that path (filesHash == ps.hash, the re-park under the NEW diffTag)
// has to line up or the landing never happens.
func TestSteerNavigateOpensAFileInALoadedCommit(t *testing.T) {
	t.Parallel()
	m, hash := navFeedModel(t) // the seed commit ADDS a.txt, so new line 18 exists
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-11", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "commit", Commit: hash},
		Line:   &steer.Line{Side: "new", No: 18},
		Wait:   true,
	})
	if m.pendingSteer == nil || m.pendingSteer.stage != steerStageFiles {
		t.Fatalf("pending = %+v, want a parked file-list stage", m.pendingSteer)
	}
	m = pumpAll(t, m, cmd)

	v := m.diffLayer()
	if v == nil {
		t.Fatal("no diff view after navigating into a commit")
	}
	row, ok := v.cursorRow()
	if !ok || row.RightNo != 18 {
		t.Errorf("cursor row = %+v ok=%v, want new line 18", row, ok)
	}
	if m.pendingSteer != nil {
		t.Errorf("pending = %+v, want it cleared once the landing happened", m.pendingSteer)
	}
	r, ok := steer.AwaitReply(dir, "n-11", 3*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "a.txt:18") {
		t.Fatalf("reply = %+v ok=%v, want ok:true detailing a.txt:18", r, ok)
	}
}

// TestSteerNavigateLandsOnTheOldSide is the other half of the {side,no} pair:
// old line 18 is the pre-edit "line 18" the unstaged diff still carries.
func TestSteerNavigateLandsOnTheOldSide(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-12", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "old", No: 18},
		Wait:   true,
	})
	m = pumpDiff(t, m, cmd)
	v := m.diffLayer()
	if v == nil {
		t.Fatal("no diff view after navigate")
	}
	row, ok := v.cursorRow()
	if !ok || row.LeftNo != 18 {
		t.Errorf("cursor row = %+v ok=%v, want OLD line 18", row, ok)
	}
	r, ok := steer.AwaitReply(dir, "n-12", 2*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "a.txt:18") {
		t.Fatalf("reply = %+v ok=%v, want ok:true detailing a.txt:18", r, ok)
	}
}

// navFeedModel is loadedNavModel with the seed commit in the feed, and returns
// its sha: the fixture for every commit-addressed navigate.
func navFeedModel(t *testing.T) (Model, string) {
	t.Helper()
	m := loadedNavModel(t)
	hash := gitOut(t, m.currentWorktree, "rev-parse", "HEAD")
	m.commits = []model.Commit{{Hash: hash, Subject: "seed"}}
	return m.rebuildCommitGraph(), hash
}

// TestSteerNavigateClearsACommitsFilterToReachTheCommit pins §4.6's go-to
// semantics for the Commits panel: a `/` filter that hides the target row must
// be dropped, not reported as "commit not loaded in the feed".
func TestSteerNavigateClearsACommitsFilterToReachTheCommit(t *testing.T) {
	t.Parallel()
	m, hash := navFeedModel(t)
	dir := m.steerDir
	m.filterPanel = panelCommits
	m.filterQuery = "no-such-subject" // hides the only row
	if len(m.displayIndices(panelCommits)) != 0 {
		t.Fatal("the fixture's filter does not actually hide the commit")
	}
	// The `\` scope and `@` highlight must SURVIVE: only the / filter is a
	// go-to obstacle.
	m.highlightQuery = "seed"
	m.commitFilter = commitFilterFields{Author: "t"}

	m, cmd := m.applySteer(steer.Command{ID: "n-13", Cmd: "navigate", Commit: hash, Wait: true})
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, "n-13", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true — a filtered-away commit is still loaded", r, ok)
	}
	if m.filterQuery != "" {
		t.Errorf("filterQuery = %q, want it cleared so the landing is visible", m.filterQuery)
	}
	if m.focus != panelCommits {
		t.Errorf("focus = %v, want panelCommits", m.focus)
	}
	idx := m.displayIndices(panelCommits)
	sel := m.sel[panelCommits]
	if sel < 0 || sel >= len(idx) {
		t.Fatalf("selection %d is outside the %d visible rows", sel, len(idx))
	}
	if c, ok := m.commitAtUnified(idx[sel]); !ok || c.Hash != hash {
		t.Errorf("selected commit = %+v, want %s", c, hash)
	}
	if m.highlightQuery == "" || !m.commitFilter.filtered() {
		t.Error("only the / filter may be dropped — the @ highlight and \\ scope must survive")
	}
}

// TestSteerFocusSupersedesAParkedNavigate: focus pops the very diff a parked
// navigate waits on, so the parked command must be answered rather than left to
// the 5 s TTL — the CLI gives up after two.
func TestSteerFocusSupersedesAParkedNavigate(t *testing.T) {
	t.Parallel()
	m, hash := navFeedModel(t)
	dir := m.steerDir
	m, _ = m.applySteer(steer.Command{
		ID: "n-14", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "commit", Commit: hash},
		Line:   &steer.Line{Side: "new", No: 18},
		Wait:   true,
	})
	if m.pendingSteer == nil {
		t.Fatal("the navigate must park")
	}
	m, cmd := m.applySteer(steer.Command{ID: "f-3", Cmd: "focus", Panel: "branches", Wait: true})
	runSteerCmd(t, cmd)
	if m.pendingSteer != nil {
		t.Error("focus moved the view — the parked navigate must not survive it")
	}
	r, ok := steer.AwaitReply(dir, "n-14", 2*time.Second)
	if !ok || r.OK || !strings.Contains(r.Error, "superseded") {
		t.Fatalf("parked reply = %+v ok=%v, want ok:false \"superseded…\"", r, ok)
	}
	fr, ok := steer.AwaitReply(dir, "f-3", 2*time.Second)
	if !ok || !fr.OK {
		t.Fatalf("focus reply = %+v ok=%v, want ok:true", fr, ok)
	}
}

// steerNotedModel is the note-jump fixture (a diff with notes on lines 5 and
// 25) with a steering inbox injected.
func steerNotedModel(t *testing.T) (Model, string) {
	t.Helper()
	m := notedModel(t)
	m.cfg = config.Defaults()
	m.ready = true
	m.loading, m.running = false, false
	m.steerDir = filepath.Join(t.TempDir(), "steer")
	m = m.initSteerInbox()
	return m, m.steerDir
}

func TestSteerStepMovesToTheNextNote(t *testing.T) {
	t.Parallel()
	m, dir := steerNotedModel(t)
	m.diffLayer().setCursorLine(0, m.diffBodyRows())
	m, cmd := m.applySteer(steer.Command{ID: "s-2", Cmd: "navigate", Step: "next_note", Wait: true})
	runSteerCmd(t, cmd)
	if got := m.diffLayer().curLine; got != 4 {
		t.Errorf("cursor at line %d, want 4 (the note on line 5)", got)
	}
	r, ok := steer.AwaitReply(dir, "s-2", time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "next") {
		t.Fatalf("reply = %+v ok=%v, want ok:true naming the direction", r, ok)
	}
}

func TestSteerStepPastTheLastNoteIsRefused(t *testing.T) {
	t.Parallel()
	m, dir := steerNotedModel(t)
	m.diffLayer().setCursorLine(24, m.diffBodyRows()) // the LAST noted line
	m, cmd := m.applySteer(steer.Command{ID: "s-3", Cmd: "navigate", Step: "next_note", Wait: true})
	runSteerCmd(t, cmd)
	if got := m.diffLayer().curLine; got != 24 {
		t.Errorf("a refused step moved the cursor to %d; it must stay put", got)
	}
	r, ok := steer.AwaitReply(dir, "s-3", time.Second)
	if !ok || r.OK || !strings.Contains(r.Error, "no next note") {
		t.Fatalf("reply = %+v ok=%v, want ok:false \"no next note in this diff\"", r, ok)
	}
}

func TestExpirePendingSteerAnswersAndClears(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.pendingSteer = &pendingSteer{
		cmd:   steer.Command{ID: "x-1", Cmd: "navigate", Wait: true},
		stage: steerStageDiff,
		at:    time.Now().Add(-2 * steerPendingTTL),
	}
	m, cmd := m.expirePendingSteer(time.Now())
	runSteerCmd(t, cmd)
	if m.pendingSteer != nil {
		t.Error("an expired pending steer must be cleared")
	}
	r, ok := steer.AwaitReply(dir, "x-1", time.Second)
	if !ok || r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:false — the CLI must never hang on a load that never arrived", r, ok)
	}
}

// loadedNavModel is a Model over navRepo with its status already loaded and its
// steering inbox injected: the starting point for every navigate test.
func loadedNavModel(t *testing.T) Model {
	t.Helper()
	dir := navRepo(t)
	m := New(domain.Open(dir))
	m.cfg = config.Defaults()
	m.currentWorktree = dir // the unstaged loader reads the new side from here
	m.steerDir = filepath.Join(t.TempDir(), "steer")
	m = m.initSteerInbox()
	m.ready = true
	m.loading = false // New() sets it true; opsIdle() would refuse every command
	m.width, m.height = 200, 60
	st, err := m.svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	m = m.withStatus(st)
	return m
}

// pumpDiff runs cmd, feeding every diffMsg (and any batched sibling) back into
// Update, which is where a parked steer lands.
func pumpDiff(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range flattenCmd(t, cmd) {
		tm, next := m.Update(msg)
		m = tm.(Model)
		if next != nil {
			m = pumpDiff(t, m, next)
		}
	}
	return m
}

// pumpAll is pumpDiff with a bounded number of rounds, for chains that go
// through a source reload (status miss → reload → retry).
func pumpAll(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 8 && cmd != nil; i++ {
		msgs := flattenCmd(t, cmd)
		cmd = nil
		for _, msg := range msgs {
			tm, next := m.Update(msg)
			m = tm.(Model)
			if next != nil {
				cmd = tea.Batch(cmd, next)
			}
		}
	}
	return m
}

// refPairRepo builds a repo with three commits, each touching a DIFFERENT
// file, and a branch "feat/x" at the tip (c3). It is the ONE shared fixture
// for both the ref and the pair navigate tests (ruling S5): a ref landing on
// feat/x must show only c3's own file (c.txt), while a pair spanning
// c1..feat/x must show the RANGE's files (b.txt and c.txt, introduced by c2
// and c3) — proving the two lanes never collapse into the same landing for
// what looks like the same tip.
func refPairRepo(t *testing.T) (dir, c1, c2, c3 string) {
	t.Helper()
	dir = gittest.BasicRepo(t, "hi\n")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a\n")
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-m", "c1")
	c1 = gitOut(t, dir, "rev-parse", "HEAD")

	write("b.txt", "b\n")
	gitRun(t, dir, "add", "b.txt")
	gitRun(t, dir, "commit", "-m", "c2")
	c2 = gitOut(t, dir, "rev-parse", "HEAD")

	write("c.txt", "c\n")
	gitRun(t, dir, "add", "c.txt")
	gitRun(t, dir, "commit", "-m", "c3")
	c3 = gitOut(t, dir, "rev-parse", "HEAD")

	gitRun(t, dir, "branch", "feat/x", c3)
	return dir, c1, c2, c3
}

// refPairModel wraps dir in a Model with a real steering inbox, ready for
// applySteer — the starting point for every ref/pair navigate test.
func refPairModel(t *testing.T, dir string) Model {
	t.Helper()
	m := New(domain.Open(dir))
	m.cfg = config.Defaults()
	m.currentWorktree = dir
	m.steerDir = filepath.Join(t.TempDir(), "steer")
	m = m.initSteerInbox()
	m.ready = true
	m.loading = false
	m.width, m.height = 200, 60
	return m
}

// advanceBranch commits name/body onto branch and returns the new tip. Used
// by TestSteerNavigateRefResolvesAtApplyTime to move a branch AFTER the
// steer.Command naming it was built.
func advanceBranch(t *testing.T, dir, branch, name, body, msg string) string {
	t.Helper()
	gitRun(t, dir, "checkout", branch)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", name)
	gitRun(t, dir, "commit", "-m", msg)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

// TestSteerNavigateRefOpensTheTipsFiles pins R2's consumer half: the wire
// carries the NAME, and the TUI resolves it, landing a single-commit files
// view on the tip's OWN changed files — never the compare view a pair would
// open (S5: contrast with TestSteerNavigatePairOpensACompare on this same
// fixture).
func TestSteerNavigateRefOpensTheTipsFiles(t *testing.T) {
	t.Parallel()
	dir, _, _, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	sdir := m.steerDir
	c := steer.Command{ID: "r-1", Cmd: "navigate", Target: &steer.Target{State: "ref", Ref: "feat/x"}, Wait: true}
	m, cmd := m.applySteer(c)
	m = pumpDiff(t, m, cmd)
	if m.filesView == nil {
		t.Fatal("no files view opened")
	}
	if m.inCompareMode() {
		t.Error("a ref landing must be a single-commit files view, never a compare")
	}
	if m.filesHash != c3 {
		t.Errorf("filesHash = %q, want the tip %q", m.filesHash, c3)
	}
	r, ok := steer.AwaitReply(sdir, "r-1", 2*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "opened feat/x") {
		t.Fatalf("reply = %+v ok=%v, want ok:true naming feat/x", r, ok)
	}
}

// TestSteerNavigateRefResolvesAtApplyTime advances the branch AFTER building
// the command and asserts the landing is the new tip. This is the test that
// earns the name-on-the-wire ruling; freezing the sha passes every other
// test in this file.
func TestSteerNavigateRefResolvesAtApplyTime(t *testing.T) {
	t.Parallel()
	dir, _, _, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	sdir := m.steerDir
	c := steer.Command{ID: "r-2", Cmd: "navigate", Target: &steer.Target{State: "ref", Ref: "feat/x"}, Wait: true}

	c4 := advanceBranch(t, dir, "feat/x", "d.txt", "d\n", "c4")
	if c4 == c3 {
		t.Fatal("fixture did not actually move the branch")
	}

	m, cmd := m.applySteer(c)
	m = pumpDiff(t, m, cmd)
	if m.filesHash != c4 {
		t.Errorf("filesHash = %q, want the NEW tip %q — the poster's captured sha %q must not win", m.filesHash, c4, c3)
	}
	r, ok := steer.AwaitReply(sdir, "r-2", 2*time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

// TestSteerNavigateRefUnknownBranchRefuses asserts an English protocol
// refusal naming the branch, and that nothing in the model moved.
func TestSteerNavigateRefUnknownBranchRefuses(t *testing.T) {
	t.Parallel()
	dir, _, _, _ := refPairRepo(t)
	m := refPairModel(t, dir)
	sdir := m.steerDir
	m = m.pushLayer(&diffView{title: "user's own diff"})
	c := steer.Command{ID: "r-3", Cmd: "navigate", Target: &steer.Target{State: "ref", Ref: "feat/nope"}, Wait: true}
	m, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	if m.filesView != nil {
		t.Error("a refused ref navigate must not have opened a files view")
	}
	if m.topLayer() == nil {
		t.Error("a refusal must leave the user's open view alone")
	}
	r, ok := steer.AwaitReply(sdir, "r-3", 2*time.Second)
	if !ok || r.OK || !strings.Contains(r.Error, "feat/nope") {
		t.Fatalf("reply = %+v ok=%v, want ok:false naming feat/nope", r, ok)
	}
}

// TestSteerNavigatePairOpensACompare asserts filesModeCompare and the two
// endpoints, and that a pair NEVER reaches openCompareFiles as a PairEndpoint
// (CacheTag would key the cache on one side of a two-sided view). S5: run
// against the SAME fixture as the ref tests above — feat/x names the exact
// tip this pair's B half resolves to, yet the landing here must be a
// compare, never the single-commit files view the ref test asserts.
func TestSteerNavigatePairOpensACompare(t *testing.T) {
	t.Parallel()
	dir, c1, _, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	sdir := m.steerDir
	c := steer.Command{ID: "pr-1", Cmd: "navigate",
		Target: &steer.Target{State: "pair", A: c1, B: "feat/x"}, Wait: true}
	m, cmd := m.applySteer(c)
	m = pumpDiff(t, m, cmd)
	if !m.inCompareMode() {
		t.Fatal("a pair navigate must open the compare view, not a single-commit files view")
	}
	if m.filesLeft.Kind() != model.EndpointCommit || m.filesLeft.Hash() != c1 {
		t.Errorf("filesLeft = %+v, want CommitEndpoint(%s)", m.filesLeft, c1)
	}
	if m.filesRight.Kind() != model.EndpointCommit || m.filesRight.Hash() != c3 {
		t.Errorf("filesRight = %+v, want CommitEndpoint(%s)", m.filesRight, c3)
	}
	if m.filesLeft.Kind() == model.EndpointPair || m.filesRight.Kind() == model.EndpointPair {
		t.Error("a pair navigate must never build a PairEndpoint: that keys the cache on one SIDE of a two-sided view")
	}
	// The RANGE, not c3's own change: b.txt (introduced by c2) and c.txt
	// (introduced by c3) are both in the change-set c1..feat/x, but a.txt is
	// NOT — it already existed at c1, so it differs from neither side.
	hasPath := func(p string) bool {
		for _, l := range m.filesView.lines {
			if l.path == p {
				return true
			}
		}
		return false
	}
	if !hasPath("b.txt") || !hasPath("c.txt") {
		t.Errorf("compare file list = %+v, want b.txt and c.txt (the RANGE c1..feat/x)", m.filesView.lines)
	}
	if hasPath("a.txt") {
		t.Error("a.txt already existed at c1 and must not appear in the c1..feat/x change-set")
	}
	r, ok := steer.AwaitReply(sdir, "pr-1", 2*time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

// TestSteerNavigatePairUnknownHalfRefuses names the unresolved half in an
// English protocol refusal.
func TestSteerNavigatePairUnknownHalfRefuses(t *testing.T) {
	t.Parallel()
	dir, c1, _, _ := refPairRepo(t)
	m := refPairModel(t, dir)
	sdir := m.steerDir
	c := steer.Command{ID: "pr-2", Cmd: "navigate",
		Target: &steer.Target{State: "pair", A: c1, B: "nope/nope"}, Wait: true}
	m, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	if m.filesView != nil {
		t.Error("a refused pair navigate must not have opened a files view")
	}
	r, ok := steer.AwaitReply(sdir, "pr-2", 2*time.Second)
	if !ok || r.OK || !strings.Contains(r.Error, "nope/nope") {
		t.Fatalf("reply = %+v ok=%v, want ok:false naming nope/nope", r, ok)
	}
}

// TestSteerNavigatePairWithFileOpensTheDiff proves the pair-plus-file pending
// actually drains. openCompareFiles sets m.compareTag, not m.filesHash's
// SIBLING gate (drainPendingFiles keys on m.filesHash, which the compare
// handler's success path never routes through drainPendingFiles/drainPendingLoad
// for) — a pending parked on the wrong gate never lands, and the CLI hangs
// out its wait instead of getting a reply.
func TestSteerNavigatePairWithFileOpensTheDiff(t *testing.T) {
	t.Parallel()
	dir, c1, _, _ := refPairRepo(t)
	m := refPairModel(t, dir)
	sdir := m.steerDir
	c := steer.Command{ID: "pr-3", Cmd: "navigate", File: "c.txt",
		Target: &steer.Target{State: "pair", A: c1, B: "feat/x"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	if m.pendingSteer == nil {
		t.Fatal("a pair-plus-file navigate must park until the compare's file list loads")
	}
	m = pumpAll(t, m, cmd)
	if m.diffLayer() == nil {
		t.Fatal("the file's diff must be open")
	}
	if m.pendingSteer != nil {
		t.Errorf("pending = %+v, want it drained once the compare's file list and diff both landed", m.pendingSteer)
	}
	r, ok := steer.AwaitReply(sdir, "pr-3", 3*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "c.txt:1") {
		t.Fatalf("reply = %+v ok=%v, want ok:true detailing c.txt:1", r, ok)
	}
}

// flattenCmd runs one tea.Cmd tree and returns every non-nil message it
// produced, batches expanded.
func flattenCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, flattenCmd(t, c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// TestSteerNavigateRefWithAFileOpensByHash is the FILE-carrying half of the
// ref lane, and it must agree with its file-less sibling
// (TestSteerNavigateRefOpensTheTipsFiles) about what a tip is.
//
// The file-less arm deliberately opens by hash for either origin, because
// steerNavigate's commit arm refuses "commit not loaded in the feed" and a
// BRANCH TIP is precisely the commit least likely to be paged in. The
// file-carrying arm delegated to steerNavigateCommitFile, whose first move is
// that same feed probe — so the two halves of one lane disagreed: no file
// landed, a file refused. This fixture never loads the feed (commitsTotal()
// is 0), which is the common case for a freshly launched `gg open`.
func TestSteerNavigateRefWithAFileOpensByHash(t *testing.T) {
	t.Parallel()
	dir, _, _, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	sdir := m.steerDir
	if m.commitsTotal() != 0 {
		t.Fatalf("fixture precondition: the feed must be empty, got %d rows", m.commitsTotal())
	}
	c := steer.Command{
		ID: "r-file-1", Cmd: "navigate",
		Target: &steer.Target{State: "ref", Ref: "feat/x"},
		File:   "c.txt", Wait: true,
	}
	m, cmd := m.applySteer(c)
	m = pumpDiff(t, m, cmd)

	r, ok := steer.AwaitReply(sdir, "r-file-1", 2*time.Second)
	if !ok {
		t.Fatal("no reply")
	}
	if !r.OK {
		t.Fatalf("refused with %q — a ref names a TREE, so its file opens by hash "+
			"exactly as the file-less arm does; the feed probe belongs to the plain-commit lane", r.Error)
	}
	if m.filesView == nil {
		t.Fatal("no files view opened")
	}
	if m.filesHash != c3 {
		t.Errorf("filesHash = %q, want the tip %q", m.filesHash, c3)
	}
	if m.diffLayer() == nil {
		t.Error("c.txt's diff must be open: the command named a file")
	}
}
