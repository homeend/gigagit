package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// previewSteerModel is mergePreviewModel wired for steering: a real inbox dir
// and the loading gate cleared, so applySteer is not refused outright.
func previewSteerModel(t *testing.T) (Model, string) {
	t.Helper()
	m, _, _ := mergePreviewModel(t)
	dir := t.TempDir()
	m.steerDir = dir
	m.snapshotWorktree = t.TempDir()
	m.loading = false
	m.width, m.height = 120, 40
	return m, dir
}

// readOneReply reads the reply the answer command wrote for id.
func readOneReply(t *testing.T, dir, id string) steer.Reply {
	t.Helper()
	r, ok := steer.AwaitReply(dir, id, 2*time.Second)
	if !ok {
		t.Fatalf("no reply for %s", id)
	}
	return r
}

// A preview navigate with a file opens the preview, then the file's diff, then
// lands the line.
func TestSteerNavigateIntoASavedPreview(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	if m.pendingSteer == nil || m.pendingSteer.stage != steerStagePreview {
		t.Fatalf("pendingSteer = %+v, want the preview stage", m.pendingSteer)
	}
	m = drainMsgs(t, m, cmd, 6)
	if m.previewOpen == nil || m.previewOpen.source != "feat/x" || m.previewOpen.target != "main" {
		t.Fatalf("previewOpen = %+v, want the commanded pair", m.previewOpen)
	}
	if m.previewOpen.id == "" {
		t.Error("a SAVED pair must open by its row id, not as a show-once")
	}
	if m.diffLayer() == nil {
		t.Fatal("the file's diff must be open")
	}
	if m.pendingSteer != nil {
		t.Errorf("pendingSteer = %+v, want it drained", m.pendingSteer)
	}
	rep := readOneReply(t, dir, "p-1")
	if !rep.OK || !strings.Contains(rep.Detail, "a.txt:1") {
		t.Errorf("reply = %+v, want ok:true detailing a.txt:1", rep)
	}
}

// No saved preview matches the pair: the TUI opens a SHOW-ONCE one (id "").
func TestSteerNavigateOpensAShowOncePreview(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	m.previews = nil // no saved rows at all
	c := steer.Command{ID: "p-2", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	if m.previewOpen == nil {
		t.Fatal("a show-once preview must still open")
	}
	if m.previewOpen.id != "" {
		t.Errorf("previewOpen.id = %q, want \"\" (show once)", m.previewOpen.id)
	}
	rep := readOneReply(t, dir, "p-2")
	if !rep.OK || !strings.Contains(rep.Detail, "a.txt:1") {
		t.Errorf("reply = %+v, want ok:true detailing a.txt:1", rep)
	}
}

// With no file the command only REVEALS the Previews entry.
func TestSteerNavigatePreviewWithNoFileRevealsTheEntry(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-3", Cmd: "navigate",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"}, Wait: true}
	m, cmd := m.applySteer(c)
	if m.focus != panelPreviews {
		t.Errorf("focus = %v, want panelPreviews", m.focus)
	}
	if m.filesView != nil {
		t.Error("a reveal must not open the compare view")
	}
	if r, ok := m.selectedPreview(); !ok || r.rec.Source != "feat/x" || r.rec.Target != "main" {
		t.Errorf("selectedPreview = %+v ok=%v, want the commanded pair selected", r, ok)
	}
	if cmd != nil {
		cmd()
	}
	rep := readOneReply(t, dir, "p-3")
	if !rep.OK || !strings.Contains(rep.Detail, "revealed preview main...feat/x") {
		t.Errorf("reply = %+v", rep)
	}
}

// A branch the repo does not have is refused in ENGLISH protocol prose.
func TestSteerNavigatePreviewMissingBranchIsRefused(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-4", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/nope", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	rep := readOneReply(t, dir, "p-4")
	if rep.OK {
		t.Fatalf("reply = %+v, want a refusal", rep)
	}
	if !strings.Contains(rep.Error, "missing branch feat/nope") {
		t.Errorf("reply.Error = %q, want English protocol prose naming the branch", rep.Error)
	}
	if m.pendingSteer != nil {
		t.Error("the pending must be cleared by the refusal")
	}
}

// A preview target with no pair is refused before anything moves.
func TestSteerPreviewTargetNeedsBothNames(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-5", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x"}, Wait: true}
	m, cmd := m.applySteer(c)
	if cmd != nil {
		cmd()
	}
	if m.pendingSteer != nil {
		t.Error("a malformed target must park nothing")
	}
	rep := readOneReply(t, dir, "p-5")
	if rep.OK || !strings.Contains(rep.Error, "source and target") {
		t.Errorf("reply = %+v", rep)
	}
}

// A path the preview does not carry is answered, not left parked.
func TestSteerNavigatePreviewUnknownPathIsAnswered(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-6", Cmd: "navigate", File: "nope.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	rep := readOneReply(t, dir, "p-6")
	if rep.OK || !strings.Contains(rep.Error, "nope.txt is not in preview main...feat/x") {
		t.Errorf("reply = %+v", rep)
	}
	if m.pendingSteer != nil {
		t.Error("the pending must be cleared")
	}
}
