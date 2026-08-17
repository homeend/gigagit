package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// 2001-02-03T04:05:06Z, rendered in the local zone by commitDateString — the
// tests compare against the same helper rather than a hardcoded stamp so they
// pass in any TZ.
const testUnix = int64(981173106)

func wantStamp(unix int64, author string) string {
	return filesMetaLine(model.Commit{UnixTime: unix, Author: author})
}

// datedFilesModel is filesModel with author + date on the feed rows, i.e. what
// the Commits panel actually holds.
func datedFilesModel() Model {
	m := filesModel()
	m.commits = []model.Commit{
		{Hash: "1111111aaaa", Subject: "one", Author: "alice", UnixTime: testUnix},
		{Hash: "2222222bbbb", Subject: "two", Author: "bob", UnixTime: testUnix + 86400},
	}
	return m
}

// The point of the feature: the date is on screen without pressing i.
func TestFilesViewShowsTheCommitDate(t *testing.T) {
	m := openFilesView(t, datedFilesModel())
	want := wantStamp(testUnix, "alice")
	if filesMetaLine(m.filesCommit) != want {
		t.Fatalf("filesMeta = %q, want %q", filesMetaLine(m.filesCommit), want)
	}
	out := m.renderFilesView(60, 20)
	if !strings.Contains(out, want) {
		t.Fatalf("rendered files view lacks the date line %q:\n%s", want, out)
	}
}

// The line sits directly under the title, not appended to it — a long subject
// must never truncate the date out of view.
func TestFilesViewDateIsItsOwnLineUnderTheTitle(t *testing.T) {
	m := datedFilesModel()
	m.commits[0].Subject = strings.Repeat("long subject ", 20)
	m = openFilesView(t, m)
	lines := strings.Split(m.renderFilesView(60, 20), "\n")
	titleAt, metaAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "Files 1111111") {
			titleAt = i
		}
		if strings.Contains(l, wantStamp(testUnix, "alice")) {
			metaAt = i
		}
	}
	if titleAt < 0 || metaAt < 0 {
		t.Fatalf("title at %d, meta at %d; want both present:\n%s", titleAt, metaAt, strings.Join(lines, "\n"))
	}
	if metaAt != titleAt+1 {
		t.Fatalf("meta line at %d, want directly under the title at %d", metaAt, titleAt)
	}
}

// Walking commits with j/k under an open view repaints the date for the commit
// now on display — a date captured once at open time would go stale here.
func TestFilesViewDateFollowsTheCommitSelection(t *testing.T) {
	m := openFilesView(t, datedFilesModel())
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("moving the selection must fire a follow-live reload")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if want := wantStamp(testUnix+86400, "bob"); filesMetaLine(m.filesCommit) != want {
		t.Fatalf("filesMeta = %q after j, want %q", filesMetaLine(m.filesCommit), want)
	}
}

// Tags / goto-SHA / the reflog synthesize a model.Commit that has only a hash.
// The date must be fetched rather than left blank — those are exactly the
// surfaces where you cannot see the date anywhere else.
func TestFilesViewFetchesTheDateWhenTheCommitCarriesNone(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit files)", gitexec.Result{
		Stdout: "M\x00internal/tui/model.go\x00",
	})
	f.SetResponse("git log", gitexec.Result{ // the CommitMeta lookup
		Stdout: "1111111aaaa\x1f\x1fcarol\x1f981173106\x1fthe real subject\x1f\x1f\n",
	})
	m := Model{
		svc:       domain.New(&git.Repo{Runner: f}),
		width:     80,
		height:    24,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		focus:     panelCommits,
	}
	m2, cmd := m.openChangedFiles(model.Commit{Hash: "1111111aaaa"}) // hash only
	if cmd == nil {
		t.Fatal("opening the files view must fire the load")
	}
	updated, _ := m2.Update(cmd())
	m2 = updated.(Model)

	if want := wantStamp(testUnix, "carol"); filesMetaLine(m2.filesCommit) != want {
		t.Fatalf("filesMeta = %q, want %q (the date must be fetched for a bare sha)", filesMetaLine(m2.filesCommit), want)
	}
	// The same lookup carries the subject, so the title stops being hash-only.
	if !strings.Contains(m2.filesTitle, "the real subject") {
		t.Errorf("title = %q, want the fetched subject in it", m2.filesTitle)
	}
}

// The i popup must agree with the header. For a commit outside the feed the
// popup used to fall back to a hash-only Commit and print "Date: (unknown)"
// under a header that now shows a real date.
func TestFilesViewCommitCarriesTheResolvedDateToTheMessagePopup(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit files)", gitexec.Result{Stdout: "M\x00a.go\x00"})
	f.SetResponse("git log", gitexec.Result{
		Stdout: "1111111aaaa\x1f\x1fcarol\x1f981173106\x1fthe real subject\x1f\x1f\n",
	})
	m := Model{
		svc:       domain.New(&git.Repo{Runner: f}),
		width:     80,
		height:    24,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		focus:     panelCommits,
	}
	m2, cmd := m.openChangedFiles(model.Commit{Hash: "1111111aaaa"}) // not in m.commits
	updated, _ := m2.Update(cmd())
	m2 = updated.(Model)

	got := m2.filesViewCommit()
	if got.UnixTime != testUnix || got.Author != "carol" {
		t.Fatalf("filesViewCommit() = %+v, want the resolved date/author", got)
	}
	if d := commitDateString(got); strings.Contains(d, "unknown") {
		t.Errorf("the i popup would show %q under a header that has a date", d)
	}
}

// A failed lookup must not paint a placeholder date: no line at all beats
// "(unknown)" sitting permanently under the title.
func TestFilesViewNoDateLineWhenTheLookupFails(t *testing.T) {
	m := openFilesView(t, filesModel()) // feed rows carry no time, fake has no CommitMeta response
	if filesMetaLine(m.filesCommit) != "" {
		t.Fatalf("filesMeta = %q, want empty when the date is unknown", filesMetaLine(m.filesCommit))
	}
	if strings.Contains(m.renderFilesView(60, 20), "unknown") {
		t.Error("files view must not render an (unknown) date placeholder")
	}
}

// Compare mode has two endpoints and no single commit date, so it keeps its
// current header — and keeps the tree row the line would have cost.
func TestFilesViewCompareModeHasNoDateLine(t *testing.T) {
	m := openFilesView(t, datedFilesModel())
	commitRows := m.filesPageRows()

	left := model.Endpoint{Kind: model.EndpointCommit, Hash: "1111111aaaa"}
	right := model.Endpoint{Kind: model.EndpointWorkTree}
	m2, _ := m.openCompareFiles(left, right)
	if got := m2.filesMetaLineFor(); got != "" {
		t.Fatalf("date line = %q in compare mode, want none", got)
	}
	// Even if a resolved commit is still sitting in the model, the mode gate
	// must keep the line off a compare header.
	m2.filesCommit = model.Commit{Hash: "1111111aaaa", Author: "alice", UnixTime: testUnix}
	if got := m2.filesMetaLineFor(); got != "" {
		t.Fatalf("date line = %q in compare mode with a resolved commit, want none", got)
	}
	m2.filesCommit = model.Commit{}
	if m2.filesPageRows() != commitRows+1 {
		t.Fatalf("compare rows = %d, commit rows = %d; the date line must cost exactly one row",
			m2.filesPageRows(), commitRows)
	}
}

// The extra line must come out of the tree's budget, not out of the box: the
// rendered panel still fits the height it was given, and the hint line (the
// bottom row) survives.
func TestFilesViewWithDateStillFitsItsBox(t *testing.T) {
	m := openFilesView(t, datedFilesModel())
	for _, h := range []int{8, 12, 20} {
		out := m.renderFilesView(60, h)
		if n := len(strings.Split(out, "\n")); n != h {
			t.Errorf("renderFilesView(60, %d) drew %d lines, want %d", h, n, h)
		}
		if !strings.Contains(out, "[esc]") {
			t.Errorf("height %d: the hint line was pushed out by the date line:\n%s", h, out)
		}
	}
}
