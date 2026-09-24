package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rowIndex(rows []actionRow, id string) (int, bool) {
	for i, r := range rows {
		if r.id == id {
			return i, true
		}
	}
	return -1, false
}

// runFileLinkRow runs the . menu's "Copy file link" row the way the menu
// does, feeds every resulting message back through Update, and returns the
// model plus whatever reached the (fake) clipboard. afterCopyLink asserts
// the row sits right after "Copy link".
func runFileLinkRow(t *testing.T, m Model, afterCopyLink bool) (Model, string) {
	t.Helper()
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	rows := availableActions(m)
	r, ok := rowByID(rows, "copy-file-link")
	if !ok {
		t.Fatal(`no "Copy file link" row`)
	}
	if afterCopyLink {
		li, lok := rowIndex(rows, "copy-link")
		fi, _ := rowIndex(rows, "copy-file-link")
		if !lok || fi != li+1 {
			t.Errorf("Copy file link at %d, want right after Copy link (%d, present=%v)", fi, li, lok)
		}
	}
	tm, cmd := r.run(m)
	return pumpAll(t, tm.(Model), cmd), copied
}

func TestCopyFileLinkCopiesTheContentLink(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t) // Files panel, a.txt selected
	m.focus = panelFiles
	m, copied := runFileLinkRow(t, m, true)
	if !strings.HasSuffix(copied, "/a.txt?view=content") {
		t.Fatalf("copied %q, want …/a.txt?view=content", copied)
	}
	if !strings.Contains(m.View(), "?view=content") {
		t.Errorf("the status bar does not show the copied link (statusMsg=%q)", m.statusMsg)
	}
}

func TestCopyFileLinkMissingFileSaysSo(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.focus = panelFiles
	if err := os.Remove(filepath.Join(m.currentWorktree, "a.txt")); err != nil {
		t.Fatal(err)
	}
	m, copied := runFileLinkRow(t, m, false)
	if copied != "" {
		t.Errorf("copied %q for a missing file, want nothing", copied)
	}
	if !strings.Contains(m.View(), "a.txt is not in the working tree") {
		t.Errorf("the screen never says the file is missing (statusMsg=%q)", m.statusMsg)
	}
}

// A commit's files view: the link drops the commit and names the file on
// disk — and a row deleted in that commit still offers the row, which then
// says the file is not in the working tree.
func TestCopyFileLinkFromTheCommitFilesView(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.txt", path: "a.txt"}}}
	m.filesTreeFocused = true
	m.filesHash = strings.Repeat("ab", 20)
	m, copied := runFileLinkRow(t, m, true)
	if !strings.HasSuffix(copied, "/a.txt?view=content") || strings.Contains(copied, "@") {
		t.Fatalf("copied %q, want the commit-less …/a.txt?view=content", copied)
	}

	d := loadedNavModel(t)
	d.filesView = &contentPopup{lines: []contentLine{{text: "gone.txt", path: "gone.txt", status: "D"}}}
	d.filesTreeFocused = true
	d.filesHash = strings.Repeat("ab", 20)
	d, copied = runFileLinkRow(t, d, false)
	if copied != "" {
		t.Errorf("copied %q for a file deleted in the commit, want nothing", copied)
	}
	if !strings.Contains(d.View(), "gone.txt is not in the working tree") {
		t.Errorf("the screen never says the file is missing (statusMsg=%q)", d.statusMsg)
	}
}

// linkPreviewModel is a commit's files view with a.txt's View-file preview
// focused, showing text (split the way the loader splits it), cursor on cur.
func linkPreviewModel(t *testing.T, text string, cur int) Model {
	t.Helper()
	m := loadedNavModel(t)
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.txt", path: "a.txt"}}}
	m.filesHash = strings.Repeat("ab", 20)
	m.filesPreview = &contentPopup{title: "a.txt", lines: fileContentLinesTok([]byte(text), nil), cur: cur}
	m.filesPreviewTag = "a.txt@" + m.filesHash
	m.filesTreeFocused = false
	return m
}

// The preview shows the disk's text: its line numbers are the file's, so
// the link carries the cursor line.
func TestCopyFileLinkFromThePreviewCarriesTheCursorLine(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	disk, err := os.ReadFile(filepath.Join(m.currentWorktree, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	m = linkPreviewModel(t, string(disk), 2)
	_, copied := runFileLinkRow(t, m, false)
	if !strings.HasSuffix(copied, "/a.txt:3?view=content") {
		t.Fatalf("copied %q, want …/a.txt:3?view=content", copied)
	}
}

// The previewed version is not what is on disk: line 3 there may be another
// line, so nothing is copied and the bottom bar says why.
func TestCopyFileLinkFromAStalePreviewSaysSo(t *testing.T) {
	t.Parallel()
	m := linkPreviewModel(t, "an older\nversion\nof a.txt\n", 2)
	m, copied := runFileLinkRow(t, m, false)
	if copied != "" {
		t.Errorf("copied %q from a preview that differs from the disk, want nothing", copied)
	}
	if !strings.Contains(m.View(), "a.txt on disk differs from this version; no link copied") {
		t.Errorf("the screen never says the versions differ (statusMsg=%q)", m.statusMsg)
	}
}
