package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
)

// prDiffModel is a model whose files view shows pull request #7's diff.
func prDiffModel(t *testing.T) Model {
	t.Helper()
	m, _, _ := mergePreviewModel(t)
	m.forgeShown, m.prs = true, testPRs()
	eps, err := m.svc.PreviewOpen(context.Background(), "feat/x", "main")
	if err != nil {
		t.Fatal(err)
	}
	set, err := m.svc.PreviewNotes(context.Background(), "feat/x", "main")
	if err != nil || !set.OK() {
		t.Fatalf("note set: %+v err %v", set, err)
	}
	nm, _ := m.Update(previewOpenMsg{source: "feat/x", target: "main", gen: m.previewGen, eps: eps, set: set, title: "PR #7 · x", prNumber: 7})
	m = nm.(Model)
	if m.previewOpen == nil || m.previewOpen.prNumber != 7 {
		t.Fatalf("previewOpen = %+v", m.previewOpen)
	}
	return m
}

func TestOpeningAPRDiffFetchesItsComments(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	eps, err := m.svc.PreviewOpen(context.Background(), "feat/x", "main")
	if err != nil {
		t.Fatal(err)
	}
	nm, cmd := m.Update(previewOpenMsg{source: "feat/x", target: "main", gen: m.previewGen, eps: eps, title: "PR #7 · x", prNumber: 7})
	mm := nm.(Model)
	if cmd == nil || !mm.prCommentsInflight {
		t.Fatalf("a PR diff must fetch its comments off-thread (cmd=%v inflight=%v)", cmd != nil, mm.prCommentsInflight)
	}
	// An ordinary preview does not.
	m2, _, _ := mergePreviewModel(t)
	nm, _ = m2.Update(previewOpenMsg{source: "feat/x", target: "main", gen: m2.previewGen, eps: eps})
	if nm.(Model).prCommentsInflight {
		t.Fatal("an ordinary preview has no forge comments to fetch")
	}
	// The re-resolve a refresh issues keeps the PR identity.
	if msg := mm.reopenPreviewCmd("", "feat/x", "main", "", "")().(previewOpenMsg); msg.prNumber != 7 {
		t.Fatalf("re-resolve lost the PR number: %+v", msg.prNumber)
	}
}

func TestPRCommentsTickRunsUnderAnOpenDiff(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prCommentsInflight = false
	m.loading = false
	t0 := time.Unix(4_000_000, 0)
	m.prCommentsLast = t0
	if _, cmd := m.prCommentsTick(t0.Add(299 * time.Second)); cmd != nil {
		t.Fatal("not due before [refresh] prs elapsed")
	}
	mm, cmd := m.prCommentsTick(t0.Add(300 * time.Second))
	if cmd == nil || !mm.prCommentsInflight {
		t.Fatal("due: the open PR's comments are re-read")
	}
	if _, again := mm.prCommentsTick(t0.Add(900 * time.Second)); again != nil {
		t.Fatal("one read at a time")
	}
	off := m
	off.cfg.Refresh = config.RefreshConfig{PRs: intPtr(0)}
	if _, cmd := off.prCommentsTick(t0.Add(time.Hour)); cmd != nil {
		t.Fatal("prs = 0 turns the comment poll off too")
	}
	busy := m
	busy.running = true
	if _, cmd := busy.prCommentsTick(t0.Add(time.Hour)); cmd != nil {
		t.Fatal("never while an op runs")
	}
	closed := m.closeFilesView()
	if _, cmd := closed.prCommentsTick(t0.Add(time.Hour)); cmd != nil {
		t.Fatal("no PR diff open: nothing to poll")
	}
}

func TestPRCommentsMsgReloadsOnlyOnChange(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prCommentsInflight = true
	nm, cmd := m.Update(prCommentsMsg{number: 7, changed: false})
	mm := nm.(Model)
	if cmd != nil || mm.prCommentsInflight {
		t.Fatalf("unchanged: nothing to do (cmd=%v inflight=%v)", cmd != nil, mm.prCommentsInflight)
	}
	m.prCommentsInflight = true
	nm, cmd = m.Update(prCommentsMsg{number: 7, changed: true})
	if cmd == nil {
		t.Fatal("changed: the file-list counts (and an open diff's notes) reload")
	}
	m.prCommentsInflight = true
	nm, cmd = m.Update(prCommentsMsg{number: 8, changed: true})
	if cmd != nil {
		t.Fatal("another PR's arrival reloads nothing")
	}
	if nm.(Model).prCommentsInflight {
		t.Fatal("a read whose view closed under it must still clear the in-flight flag, or no later PR could fetch")
	}
	// A manual refresh that failed says so; a background one stays quiet.
	m.prCommentsInflight = true
	nm, _ = m.Update(prCommentsMsg{number: 7, manual: true, err: errors.New("HTTP 502\nx")})
	if got := nm.(Model).statusMsg; got == "" {
		t.Fatal("a failed manual refresh must report")
	}
}

func TestRAndIInsideAPRFilesView(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prCommentsInflight = false
	nm, cmd := m.Update(keyMsg("r"))
	if cmd == nil || !nm.(Model).prCommentsInflight {
		t.Fatal("r in a PR files view re-reads its comments")
	}
	nm, cmd = m.Update(keyMsg("i"))
	mm := nm.(Model)
	if layerOf[*prHubPopup](mm) == nil || cmd == nil {
		t.Fatal("i in a PR files view opens the hub")
	}
	nm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := nm.(Model); layerOf[*prHubPopup](got) != nil || got.filesView == nil {
		t.Fatal("esc closes the hub back onto the PR's file list")
	}
}

// The preview surface's Copy link already covers a PR diff: the pair is
// <base>...refs/gg/pr/<n>, which any checkout resolves after `gg pr fetch <n>`.
func TestPRDiffLinkNamesThePrivateRef(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	text, ok := m.previewLinkFor("refs/gg/pr/7", "main", "a.go", 3)
	if !ok {
		t.Fatal("a PR pair must be linkable")
	}
	l, err := model.ParseLink(text)
	if err != nil {
		t.Fatalf("the copied link %q does not parse: %v", text, err)
	}
	if !strings.Contains(text, "main...refs/gg/pr/7") {
		t.Fatalf("link = %q (parsed %+v)", text, l)
	}
}
