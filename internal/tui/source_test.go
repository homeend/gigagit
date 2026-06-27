package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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

func TestReadSourceBranchesProducesMsg(t *testing.T) {
	m := newTestModel(t) // wired to a real temp repo (newRepo(t))
	msg := m.readSourceCmd(srcBranches, true)()
	dm, ok := msg.(dataAvailableMsg)
	if !ok {
		t.Fatalf("want dataAvailableMsg, got %T", msg)
	}
	if dm.source != srcBranches || dm.gen != m.srcGen[srcBranches] || !dm.manual {
		t.Fatalf("bad envelope: %+v", dm)
	}
	if _, ok := dm.value.([]model.Branch); !ok {
		t.Fatalf("branches value type = %T, want []model.Branch", dm.value)
	}
}

func TestReadSourceStatusCarriesConflict(t *testing.T) {
	m := newTestModel(t)
	msg := m.readSourceCmd(srcStatus, false)().(dataAvailableMsg)
	if _, ok := msg.value.(statusPayload); !ok {
		t.Fatalf("status value type = %T, want statusPayload", msg.value)
	}
}

func TestReadSourceWorktreesCarriesPayload(t *testing.T) {
	m := newTestModel(t)
	msg := m.readSourceCmd(srcWorktrees, true)().(dataAvailableMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	p, ok := msg.value.(worktreesPayload)
	if !ok {
		t.Fatalf("worktrees value type = %T, want worktreesPayload", msg.value)
	}
	if len(p.worktrees) == 0 {
		t.Fatal("expected at least one worktree")
	}
	// headTimes is best-effort; a fresh repo may have no commits, so we only
	// assert the map is non-nil (the Snapshot arm does the same).
	if p.headTimes == nil {
		t.Fatal("headTimes must be non-nil (may be empty)")
	}
}
