package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/i18n"
)

// watchModel is loadedNavModel with name (content) on disk, open in the
// full-screen viewer and loaded.
func watchModel(t *testing.T, name, content string) (Model, *openFile) {
	t.Helper()
	m := loadedNavModel(t)
	writeWT(t, m, name, content)
	nm, cmd := m.openFileViewer(name, 0)
	m = pumpAll(t, nm, cmd)
	d := m.openFiles.find(m.currentWorktree, docKey(fileSource{kind: srcWorktree}, name))
	if d == nil {
		t.Fatal("the opened file is not in the open-files list")
	}
	return m, d
}

func writeWT(t *testing.T, m Model, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.currentWorktree, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeWT(t *testing.T, m Model, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(m.currentWorktree, name)); err != nil {
		t.Fatal(err)
	}
}

func deletedPlaceholder(d *openFile) bool {
	return len(d.p.lines) == 1 && d.p.lines[0].text == i18n.T("(file deleted on disk)")
}

func TestLoadDocStampsTheDiskState(t *testing.T) {
	t.Parallel()
	_, d := watchModel(t, "w.txt", "one\ntwo\n")
	if !d.disk.known || d.disk.missing || d.disk.size != int64(len("one\ntwo\n")) {
		t.Fatalf("disk = %+v, want the file's stat", d.disk)
	}
	if d.loading {
		t.Fatal("still loading after the fill")
	}
}

func TestLoadDocOfAMissingFileShowsDeleted(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "one\n")
	removeWT(t, m, "w.txt")
	m = pumpAll(t, m, m.loadDoc(d))
	if !deletedPlaceholder(d) || !d.disk.missing {
		t.Fatalf("lines = %+v disk = %+v, want the deleted placeholder", d.p.lines, d.disk)
	}
}

func TestPlaceholderFillKeepsTheSavedPlace(t *testing.T) {
	t.Parallel()
	d := wtDoc("x")
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(50)}, 10, 80)
	d.p.cur, d.p.sel = 29, 20
	d.fill(fileContentMsg{tag: d.tag, lines: []contentLine{{text: "(file deleted on disk)"}}, reload: true}, 10, 80)
	d.keepPlace() // a bringToFront on the placeholder must not overwrite the saved place
	if d.keep.line != 30 {
		t.Fatalf("saved line = %d, want 30", d.keep.line)
	}
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(50), reload: true}, 10, 80)
	if d.p.cur != 29 || d.p.sel != 20 {
		t.Fatalf("cur/sel = %d/%d, want 29/20", d.p.cur, d.p.sel)
	}
}

func TestBackgroundDocReceivesItsLoad(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "old\n")
	m = fvKeys(t, m, keyCtrlBracket())
	if m.docShown(d) {
		t.Fatal("ctrl+] left the file on screen")
	}
	writeWT(t, m, "w.txt", "new\n")
	tm, _ := m.Update(m.loadDoc(d)())
	m = tm.(Model)
	if d.p.lines[0].raw != "new" {
		t.Fatalf("background doc not filled: %+v", d.p.lines[0])
	}
	m = m.closeDoc(d)
	writeWT(t, m, "w.txt", "newer\n")
	m.Update(m.loadDoc(d)()) // a closed document's result finds nothing
	if d.p.lines[0].raw != "new" {
		t.Fatalf("closed doc was filled: %+v", d.p.lines[0])
	}
}

func TestLoadingFlagSetUntilFill(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "a\n")
	cmd := m.loadDoc(d)
	if !d.loading {
		t.Fatal("loadDoc did not mark the doc loading")
	}
	m.Update(cmd())
	if d.loading {
		t.Fatal("the fill did not clear loading")
	}
}

func TestFindTagSearchesEveryWorktree(t *testing.T) {
	t.Parallel()
	r := &openFilesReg{}
	a, b := wtDoc("a"), wtDoc("b")
	r.touch("wt1", a, noneShown)
	r.touch("wt2", b, noneShown)
	if r.findTag(a.tag) != a || r.findTag(b.tag) != b || r.findTag("nope") != nil {
		t.Fatal("findTag did not search every worktree")
	}
	var nilReg *openFilesReg
	if nilReg.findTag(a.tag) != nil {
		t.Fatal("nil registry found a doc")
	}
}

// tick runs one poll at now and everything it sends.
func tick(t *testing.T, m Model, now time.Time) Model {
	t.Helper()
	nm, cmd := m.openFilesTick(now)
	return pumpAll(t, nm, cmd)
}

func TestShownFileReloadsAndKeepsPlace(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", numberedLines(200))
	d.p.cur, d.p.sel = 99, 90
	d.p.lsel.start(95)
	d.p.search.query = "line 100" // the reader sits on the hit (a re-found search lands the cursor on it)
	writeWT(t, m, "w.txt", numberedLines(210))
	m = tick(t, m, time.Now())
	if n := len(d.p.lines); n != 210 {
		t.Fatalf("lines = %d, want 210 (reloaded)", n)
	}
	if d.p.cur != 99 || d.p.sel != 90 {
		t.Fatalf("cur/sel = %d/%d, want 99/90", d.p.cur, d.p.sel)
	}
	if d.p.lsel.on {
		t.Fatal("the selection survived a reload")
	}
	if len(d.p.search.hits) != 1 {
		t.Fatalf("search hits = %v, want the one re-found", d.p.search.hits)
	}
}

func TestUnchangedFileDoesNotReload(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "a\nb\n")
	t0 := time.Now()
	m = tick(t, m, t0)
	d.p.lines[0].text = "SENTINEL"
	m = tick(t, m, t0.Add(time.Second))
	if d.p.lines[0].text != "SENTINEL" {
		t.Fatal("an unchanged file was reloaded")
	}
}

func TestBackgroundFilePollsEveryFiveSeconds(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "old\n")
	t0 := time.Now()
	m = tick(t, m, t0) // the viewer's tick: checked = t0
	m = fvKeys(t, m, keyCtrlBracket())
	writeWT(t, m, "w.txt", "newer\n")
	m = tick(t, m, t0.Add(time.Second))
	if d.p.lines[0].raw != "old" {
		t.Fatal("a background file was polled after 1 s")
	}
	m = tick(t, m, t0.Add(6*time.Second))
	if d.p.lines[0].raw != "newer" {
		t.Fatalf("a background file was not reloaded after 6 s: %+v", d.p.lines[0])
	}
}

func TestCommitAndShelfDocsAreNotWatched(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	for _, src := range []fileSource{{kind: srcCommit, rev: "abc"}, {kind: srcShelf, rev: "s1"}} {
		m.openFiles.touch(m.currentWorktree, newOpenFile(src, "x.txt"), noneShown)
	}
	if n := len(m.watchedDocs()); n != 0 {
		t.Fatalf("watched %d commit/shelf docs", n)
	}
	if _, cmd := m.openFilesTick(time.Now()); cmd != nil {
		t.Fatal("a tick over commit/shelf docs sent a stat")
	}
}

func TestDeletedThenRecreated(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", numberedLines(30))
	d.p.cur = 19
	t0 := time.Now()
	removeWT(t, m, "w.txt")
	m = tick(t, m, t0)
	if !deletedPlaceholder(d) {
		t.Fatalf("lines = %+v, want the deleted placeholder", d.p.lines)
	}
	nm, cmd := m.bringToFront(d)
	m = pumpAll(t, nm, cmd)
	if !deletedPlaceholder(d) {
		t.Fatal("bringToFront of a deleted file did not keep the placeholder")
	}
	writeWT(t, m, "w.txt", numberedLines(31))
	m = tick(t, m, t0.Add(time.Second))
	if len(d.p.lines) != 31 || d.p.cur != 19 {
		t.Fatalf("lines/cur = %d/%d, want 31/19 (back at the reader's line)", len(d.p.lines), d.p.cur)
	}
}

func TestInFlightDocIsSkipped(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "a\n")
	before := d.disk
	d.loading = true
	writeWT(t, m, "w.txt", "abc\n")
	if _, cmd := m.openFilesTick(time.Now()); cmd != nil {
		t.Fatal("a doc with a load in flight was polled")
	}
	if !d.disk.same(before) {
		t.Fatal("the disk state moved without a load")
	}
}

func TestStaleStatMsgDropped(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "a\n")
	writeWT(t, m, "w.txt", "abc\n")
	nm, cmd := m.openFilesTick(time.Now())
	msg := cmd()
	nm.docWatch.gen++ // a worktree switch happened meanwhile
	tm, next := nm.Update(msg)
	if next != nil || d.p.lines[0].raw != "a" {
		t.Fatal("a stale stat result reloaded the file")
	}
	if tm.(Model).docWatch.polling {
		t.Fatal("a stale result left the poll marked in flight")
	}
}

func TestNoPollWhileSwitching(t *testing.T) {
	t.Parallel()
	m, _ := watchModel(t, "w.txt", "a\n")
	m.loading = true
	if _, cmd := m.openFilesTick(time.Now()); cmd != nil {
		t.Fatal("polled during a repo switch")
	}
}

func TestOtherWorktreesDocsNotPolled(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.openFiles.touch("/other", wtDoc("x.txt"), noneShown)
	if n := len(m.watchedDocs()); n != 0 {
		t.Fatalf("watched %d docs of another worktree", n)
	}
}

func TestPendingLineSurvivesAPoll(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	writeWT(t, m, "w.txt", numberedLines(40))
	m, load := m.openFileViewer("w.txt", 25)
	if _, cmd := m.openFilesTick(time.Now()); cmd != nil {
		t.Fatal("a poll raced the open's load")
	}
	m = pumpAll(t, m, load)
	if fv := layerOf[*fileViewer](m); fv == nil || fv.p.cur != 24 {
		t.Fatal("the link's line did not land")
	}
}

// withDocWatch builds and stores the fsnotify watcher over m's open files, as
// a tick on a supported filesystem would. The listen loop is NOT started (it
// blocks); tests read the watcher's events directly.
func withDocWatch(t *testing.T, m Model) Model {
	t.Helper()
	m.watchSupported = true
	nm, cmd := m.syncDocWatch()
	if cmd == nil {
		t.Fatal("no watcher build on a supported filesystem")
	}
	tm, _ := nm.Update(cmd())
	m = tm.(Model)
	if m.docWatch.w == nil {
		t.Fatal("the built watcher was not stored")
	}
	w := m.docWatch.w
	t.Cleanup(func() { _ = w.Close() })
	return m
}

func TestDocWatchBuildsOnSupportedFS(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "a\n")
	m = withDocWatch(t, m)
	writeWT(t, m, "w.txt", "abc\n")
	select {
	case got := <-m.docWatch.w.Events():
		if got != m.docAbs(d) {
			t.Fatalf("event %q, want %q", got, m.docAbs(d))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no fsnotify event for the open file")
	}
}

func TestDocWatchEventPollsABackgroundFileNow(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "old\n")
	m = tick(t, m, time.Now())
	m = fvKeys(t, m, keyCtrlBracket())
	writeWT(t, m, "w.txt", "newer\n")
	tm, cmd := m.Update(docWatchEventMsg{gen: m.docWatch.gen, path: m.docAbs(d)})
	pumpAll(t, tm.(Model), cmd)
	if d.p.lines[0].raw != "newer" {
		t.Fatalf("an fsnotify event did not poll the background file at once: %+v", d.p.lines[0])
	}
}

func TestDocWatchOffWhenUnsupported(t *testing.T) {
	t.Parallel()
	m, _ := watchModel(t, "w.txt", "a\n")
	if _, cmd := m.syncDocWatch(); cmd != nil {
		t.Fatal("a watcher was built on an unsupported filesystem")
	}
}

func TestDocWatchClosesWhenNoFilesLeft(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "a\n")
	m = withDocWatch(t, m)
	w := m.docWatch.w
	m = m.closeDoc(d)
	m, _ = m.openFilesTick(time.Now())
	if m.docWatch.w != nil {
		t.Fatal("the watcher outlived the last open file")
	}
	select {
	case _, ok := <-w.Events():
		if ok {
			t.Fatal("got an event, want the channel closed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the watcher was not closed")
	}
}

func TestReRootClosesDocWatch(t *testing.T) {
	t.Parallel()
	m, _ := watchModel(t, "w.txt", "a\n")
	m = withDocWatch(t, m)
	gen := m.docWatch.gen
	tm, _ := m.reRoot(m.currentWorktree)
	if nm := tm.(Model); nm.docWatch.w != nil || nm.docWatch.gen == gen {
		t.Fatalf("reRoot left the watcher (w=%v, gen %d→%d)", nm.docWatch.w, gen, nm.docWatch.gen)
	}
}
