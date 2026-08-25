package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
)

// hasRemoteTagsItem reports whether remoteTagsItem is present in queue.
func hasRemoteTagsItem(queue []refreshItem) bool {
	for _, it := range queue {
		if it == remoteTagsItem {
			return true
		}
	}
	return false
}

// TestSrcTagsEnqueuesRemoteTagsWhenAutoOn verifies that, with default config
// (DisableRemoteTagsAuto=false) and a non-empty tag list, a srcTags arrival
// enqueues remoteTagsItem in bgQueue.
func TestSrcTagsEnqueuesRemoteTagsWhenAutoOn(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	// Default: auto enabled.
	if m.cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("precondition: DisableRemoteTagsAuto must be false (auto on)")
	}
	nm, _ := m.Update(dataAvailableMsg{
		source: srcTags,
		gen:    m.srcGen[srcTags],
		value:  []model.Tag{{Name: "v1"}},
	})
	if !hasRemoteTagsItem(nm.(Model).bgQueue) {
		t.Fatal("expected remoteTagsItem in bgQueue when auto enabled and tags present")
	}
}

// TestSrcTagsNoEnqueueWhenAutoOff verifies that when DisableRemoteTagsAuto=true
// no remoteTagsItem is enqueued on srcTags arrival.
func TestSrcTagsNoEnqueueWhenAutoOff(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.cfg.Refresh.DisableRemoteTagsAuto = true
	nm, _ := m.Update(dataAvailableMsg{
		source: srcTags,
		gen:    m.srcGen[srcTags],
		value:  []model.Tag{{Name: "v1"}},
	})
	if hasRemoteTagsItem(nm.(Model).bgQueue) {
		t.Fatal("expected remoteTagsItem NOT in bgQueue when auto disabled")
	}
}

// TestSrcTagsNoEnqueueWhenNoTags verifies that when the arriving tag list is
// empty, no remoteTagsItem is enqueued (nothing to annotate).
func TestSrcTagsNoEnqueueWhenNoTags(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	if m.cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("precondition: DisableRemoteTagsAuto must be false (auto on)")
	}
	nm, _ := m.Update(dataAvailableMsg{
		source: srcTags,
		gen:    m.srcGen[srcTags],
		value:  []model.Tag{},
	})
	if hasRemoteTagsItem(nm.(Model).bgQueue) {
		t.Fatal("expected remoteTagsItem NOT in bgQueue when tag list is empty")
	}
}

// TestAutoRemoteTagsEnabledHelper verifies the autoRemoteTagsEnabled helper
// reflects the inverted polarity of DisableRemoteTagsAuto.
func TestAutoRemoteTagsEnabledHelper(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	// Default: field false → auto enabled.
	if !m.autoRemoteTagsEnabled() {
		t.Fatal("default: autoRemoteTagsEnabled() must be true")
	}
	m.cfg = config.Config{}
	m.cfg.Refresh.DisableRemoteTagsAuto = true
	if m.autoRemoteTagsEnabled() {
		t.Fatal("DisableRemoteTagsAuto=true must make autoRemoteTagsEnabled() false")
	}
}
