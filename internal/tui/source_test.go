package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// newTestModel returns a freshly constructed Model via New(), the canonical
// constructor. There is no pre-existing helper with this name in the suite;
// this is the thin wrapper the registry tests need to prove New() initializes
// the per-source state maps.
func newTestModel(t *testing.T) Model {
	t.Helper()
	return New(domain.New(newRepo(t)))
}

func TestSrcConsumersCoverAllSources(t *testing.T) {
	for s := sourceKey(0); s < srcCount; s++ {
		if s == srcIdentity {
			continue // identity feeds the Settings popup, not a left panel
		}
		if len(srcConsumers[s]) == 0 {
			t.Errorf("source %d has no consumer panels", s)
		}
	}
}

func TestRegistryMapsInitialized(t *testing.T) {
	m := newTestModel(t) // existing helper used across *_test.go
	if m.srcGen == nil || m.srcInflight == nil || m.srcLoading == nil {
		t.Fatal("registry maps must be initialized by the constructor")
	}
}
