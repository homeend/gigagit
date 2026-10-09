package tui

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// The stacked view is the same diff drawn with its siblings: every `.` menu
// row a single-file diff offers on a line must be offered on that line in the
// stack too, and the other way round — a row that reads the stack's top view
// instead of the cursor's file silently vanishes in one mode (the stacked
// note rows, the stacked PR number, the stacked remark rows all did). These
// tests open the same diff both ways, land on the same note, and compare the
// whole menu; stackOnlyRows / singleOnlyRows name the rows that differ BY
// DESIGN, each with its reason, so a new difference is a failure to explain.

// stackOnlyRows are offered only in a stack: they act on the stack itself.
var stackOnlyRows = map[string]string{
	"stack-fold":     "folds the file under the cursor inside the stack; a single-file view has nothing to fold",
	"stack-fold-all": "folds every file of the stack",
	"stack-jump":     "jumps to another file of the stack; single-file, N/P step files instead",
}

// singleOnlyRows are offered only single-file.
var singleOnlyRows = map[string]string{}

func parityMenuIDs(m Model) []string {
	var ids []string
	for _, r := range availableActions(m) {
		ids = append(ids, r.id)
	}
	sort.Strings(ids)
	return ids
}

// diffMenuParity runs open twice — single, then stacked — lands on the first
// note of the opened diff, and compares the two menus; want is a row that
// must be in both (the context's own link row, R13).
func diffMenuParity(t *testing.T, name, want string, fresh func(t *testing.T) Model, open func(t *testing.T, m Model) Model) {
	t.Helper()
	menus := map[bool][]string{}
	for _, stacked := range []bool{false, true} {
		m := fresh(t)
		m = tempPromptStore(t, m) // never the machine's store: the flag must not leak into it, or out of it
		m = m.setStackedPref(stacked)
		m = open(t, m)
		v := m.diffLayer()
		if v == nil {
			t.Fatalf("%s: no diff opened (stacked=%v)", name, stacked)
		}
		if (v.stk != nil) != stacked {
			t.Fatalf("%s: opened stacked=%v, want %v", name, v.stk != nil, stacked)
		}
		var moved bool
		if m, moved = m.landOnNote(1); !moved {
			t.Fatalf("%s: no note to land on (stacked=%v)", name, stacked)
		}
		if !m.diffLayer().cursorOnNote() {
			t.Fatalf("%s: the cursor must rest on the note's line (stacked=%v)", name, stacked)
		}
		menus[stacked] = parityMenuIDs(m)
	}
	single, stack := menus[false], menus[true]
	has := func(ids []string, id string) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	var bad []string
	for _, id := range single {
		if !has(stack, id) && singleOnlyRows[id] == "" {
			bad = append(bad, "missing in the stack: "+id)
		}
	}
	for _, id := range stack {
		if !has(single, id) && stackOnlyRows[id] == "" {
			bad = append(bad, "missing single-file: "+id)
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%s: the . menu differs between single and stacked:\n  %s\nsingle:  %s\nstacked: %s",
			name, strings.Join(bad, "\n  "), strings.Join(single, ","), strings.Join(stack, ","))
	}
	if want != "" && !has(single, want) {
		t.Fatalf("%s: the . menu lacks %s: %s", name, want, strings.Join(single, ","))
	}
}

// A review's diff, cursor on a remark.
func TestDiffMenuParityReviewRemark(t *testing.T) {
	t.Parallel()
	diffMenuParity(t, "review remark", "copy-link",
		func(t *testing.T) Model { m, _ := openedReviewView(t); return m },
		func(t *testing.T, m Model) Model { return openReviewDiff(t, m, "a.go") })
}

// A commit's diff, cursor on a hand-written note.
func TestDiffMenuParityCommitNote(t *testing.T) {
	t.Parallel()
	diffMenuParity(t, "commit note", "note-copy-link",
		func(t *testing.T) Model {
			m := stackRepoModel(t)
			m.svc.UseNotesDir(t.TempDir())
			sha := m.commits[0].Hash
			if _, err := m.svc.NoteAdd(context.Background(), model.Note{
				Address: model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "b.go"},
				Side:    model.NoteSideNew, Range: [2]int{3, 3}, Summary: "on b.go",
			}); err != nil {
				t.Fatalf("NoteAdd: %v", err)
			}
			return m
		},
		func(t *testing.T, m Model) Model {
			l, _ := filesLine(t, m, "b.go")
			u, cmd := m.openDiffForFileLine(l)
			return drainCmds(t, u.(Model), cmd)
		})
}

// The working tree's diff, cursor on a note on an unstaged line.
func TestDiffMenuParityWorktreeNote(t *testing.T) {
	diffMenuParity(t, "worktree note", "note-copy-link",
		func(t *testing.T) Model {
			m := loadedNavModel(t)
			m.svc.UseNotesDir(t.TempDir())
			if _, err := m.svc.NoteAdd(context.Background(), model.Note{
				Address: model.FileAddress{State: model.StateUnstaged, Worktree: m.currentWorktree, Path: "a.txt"},
				Side:    model.NoteSideNew, Range: [2]int{18, 18}, Summary: "on 18",
			}); err != nil {
				t.Fatalf("NoteAdd: %v", err)
			}
			return m
		},
		func(t *testing.T, m Model) Model {
			m, cmd := m.applySteer(steer.Command{
				ID: "par-1", Cmd: "navigate", File: "a.txt",
				Target: &steer.Target{State: "unstaged"},
				Line:   &steer.Line{Side: "new", No: 18},
				Wait:   true,
			})
			return pumpDiff(t, m, cmd)
		})
}

// A pull request's diff, cursor on a local note written for the PR: the forge
// rows (Send as GitHub comment, …) must survive the stack too.
func TestDiffMenuParityPRNote(t *testing.T) {
	t.Parallel()
	diffMenuParity(t, "PR note", "note-copy-link",
		func(t *testing.T) Model {
			m, _, _ := mergePreviewModel(t)
			m.svc.UseNotesDir(t.TempDir())
			m.forgeShown, m.prs = true, testPRs()
			eps, err := m.svc.PreviewOpen(context.Background(), "feat/x", "main")
			if err != nil {
				t.Fatal(err)
			}
			set, err := m.svc.PreviewNotes(context.Background(), "feat/x", "main")
			if err != nil || !set.OK() {
				t.Fatalf("note set: %+v err %v", set, err)
			}
			if _, err := m.svc.NoteAdd(context.Background(), model.Note{
				Address: model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: "a.txt"},
				Preview: set.Pair(), Side: model.NoteSideNew, Range: [2]int{1, 1}, Summary: "on the PR",
			}); err != nil {
				t.Fatalf("NoteAdd: %v", err)
			}
			// The open's cmd loads the PR's file list (compareFilesMsg); the
			// comments fetch it batches is answered by the fake forge.
			u, cmd := m.Update(previewOpenMsg{source: "feat/x", target: "main", gen: m.previewGen, eps: eps, set: set, title: "PR #7 · x", prNumber: 7})
			m = drainCmds(t, u.(Model), cmd)
			if m.previewOpen == nil || m.previewOpen.prNumber != 7 {
				t.Fatalf("previewOpen = %+v", m.previewOpen)
			}
			return m
		},
		func(t *testing.T, m Model) Model {
			l, _ := filesLine(t, m, "a.txt")
			u, cmd := m.openDiffForFileLine(l)
			return drainCmds(t, u.(Model), cmd)
		})
}
