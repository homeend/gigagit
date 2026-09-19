package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Two compare entry points read a commit hash back out of a STORE rather than
// off a git feed row, and neither store validates on read:
//
//   - a bookmark's Commit comes from a machine-local TOML file that an older
//     gg wrote and a human can edit;
//   - a branch version's Base/Ours are whitespace-split out of a ref trailer
//     by git.ParseVersionMeta, which checks the FIELD COUNT and nothing else.
//
// Both used to hand that value straight to mustCommitEndpoint, so one stale
// or hand-edited record panicked the whole TUI — the same shape as the
// core.abbrev regression, where a legal git setting made every short hash
// fail model.CommitEndpoint's 7..64 floor. A bad record must decline with a
// notice and leave the popup standing.

// badStoreHashes are the shapes a store can hand back that CommitEndpoint
// refuses: too short (core.abbrev, or an old record that stored %h), empty,
// and not hex at all.
var badStoreHashes = []string{"abc1", "", "not-a-sha", strings.Repeat("f", 65)}

func TestCommitBookmarkComparDeclinesOnAnUnusableStoredHash(t *testing.T) {
	t.Parallel()
	for _, bad := range badStoreHashes {
		t.Run("hash="+bad, func(t *testing.T) {
			t.Parallel()
			m := loadedModelLinearCommits(t, 3)
			m.focus = panelCommits
			m = m.pushLayer(&bookmarkPopup{})

			got, _ := m.compareCommitBookmark(model.Bookmark{Commit: bad})

			if got.filesView != nil {
				t.Fatal("an unusable stored commit must not open a comparison")
			}
			if got.statusMsg == "" {
				t.Fatal("an unusable stored commit must leave a notice")
			}
			if got.bookmarkSwitcher() == nil {
				t.Fatal("the switcher must stay up so the user can pick another bookmark")
			}
		})
	}
}

// The good path still works: a full sha from the store opens the compare.
func TestCommitBookmarkCompareStillOpensOnAGoodHash(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m = m.pushLayer(&bookmarkPopup{})

	// The open is two steps now: the sides resolve off the UI thread first
	// (a bookmark whose commit is gone must be a notice, not a broken view),
	// and the resolved pair opens the comparison.
	got, cmd := m.compareCommitBookmark(model.Bookmark{Commit: m.commits[2].Hash})
	if cmd == nil || got.statusMsg != "" {
		t.Fatalf("a valid stored commit must dispatch the resolve (cmd=%v msg=%q)", cmd, got.statusMsg)
	}
	left, _ := model.CommitEndpoint(m.commits[2].Hash)
	right, _ := model.CommitEndpoint(m.commits[0].Hash)
	mm, _ := got.Update(entryCompareMsg{gen: got.entryCompareGen, left: left, right: right})
	got = mm.(Model)

	if got.filesView == nil || !got.inCompareMode() {
		t.Fatal("a valid stored commit must open the comparison")
	}
}

func TestVersionPreviewDeclinesOnAnUnusableRecordedHash(t *testing.T) {
	t.Parallel()
	// "" is excluded on purpose: an empty Base is not a corrupt record, it is
	// a FIELDLESS one (amend/reset/undo-commit), and that arm correctly opens
	// the snapshot's own commit view instead of a preview.
	for _, bad := range []string{"abc1", "not-a-sha", strings.Repeat("f", 65)} {
		t.Run("hash="+bad, func(t *testing.T) {
			t.Parallel()
			m := Model{width: 120, height: 40}
			p := &versionsPopup{
				mode:   versionsModeVersions,
				branch: "main",
				rows: []model.BranchVersion{{
					Ref:     "refs/gg/versions/main/1753100000-rebase",
					Hash:    "5aac0000000000",
					Subject: "did a rebase",
					Op:      "rebase",
					Unix:    1753100000,
					Base:    bad,
					Ours:    "0ded0000000000",
				}},
			}
			m = m.pushLayer(p)

			got := pressKey(t, m, "enter")

			if got.filesView != nil {
				t.Fatal("an unusable recorded base must not open the frozen preview")
			}
			if got.statusMsg == "" {
				t.Fatal("an unusable recorded base must leave a notice")
			}
			if layerOf[*versionsPopup](got) != p {
				t.Fatal("the versions popup must stay up")
			}
		})
	}
}
