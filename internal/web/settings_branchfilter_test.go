package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/branchfilter"
)

// bfSettings is the branch-filter slice of the settings payload — enough of
// each slot to tell "written", "refused" and "removed" apart.
type bfSettings struct {
	BranchFilters []struct {
		Slot   int    `json:"slot"`
		Name   string `json:"name"`
		Label  string `json:"label"`
		Mode   string `json:"mode"`
		Usable bool   `json:"usable"`
		Prefix string `json:"prefix"`
		Scope  string `json:"scope"`
	} `json:"branch_filters"`
	Warnings []string `json:"branch_filter_warnings"`
}

// bfGet re-reads the settings payload into a FRESH value every time. Decoding
// into a reused struct would be a lie here: slotWire's scope and error are
// omitempty, so an unset slot leaves the previous read's value standing.
func bfGet(t *testing.T, ts *httptest.Server) bfSettings {
	t.Helper()
	var got bfSettings
	getJSON(t, ts, "/api/settings", &got)
	if len(got.BranchFilters) != branchfilter.MaxSlots {
		t.Fatalf("branch_filters = %+v; want %d slots", got.BranchFilters, branchfilter.MaxSlots)
	}
	return got
}

func TestSettingsBranchFilterWriteAndRemove(t *testing.T) {
	ts, _ := bfServer(t) // the fixture repo defines slots 1, 2 and 4
	post := func(body string) int {
		t.Helper()
		return postJSON(t, ts, "/api/settings", body, "application/json", "", nil)
	}
	got := bfGet(t, ts)
	if len(got.BranchFilters) != 5 || got.BranchFilters[0].Prefix != "feat/" || got.BranchFilters[0].Scope != "repo" {
		t.Fatalf("GET: %+v", got.BranchFilters)
	}
	if got.Warnings == nil {
		t.Errorf("warnings must be [] on the wire, never null")
	}
	if code := post(`{"branch_filters":[{"slot":5,"scope":"repo","name":"wip","mode":"show","suffix":"-wip"}]}`); code != 200 {
		t.Fatalf("POST: %d", code)
	}
	got = bfGet(t, ts)
	if got.BranchFilters[4].Name != "wip" || !got.BranchFilters[4].Usable || got.BranchFilters[4].Scope != "repo" {
		t.Errorf("slot 5 after write: %+v", got.BranchFilters[4])
	}
	// Bad regex refuses the WHOLE request and writes nothing.
	bad := `{"branch_filters":[{"slot":3,"scope":"repo","prefix":"x"},{"slot":5,"scope":"repo","regex":"("}]}`
	if code := post(bad); code != 400 {
		t.Errorf("bad regex → %d; want 400", code)
	}
	got = bfGet(t, ts)
	if got.BranchFilters[2].Usable || got.BranchFilters[4].Name != "wip" {
		t.Errorf("a refused batch wrote something: %+v", got.BranchFilters)
	}
	// Empty rule refused; remove works.
	if code := post(`{"branch_filters":[{"slot":3,"scope":"repo"}]}`); code != 400 {
		t.Errorf("empty rule → %d; want 400", code)
	}
	if code := post(`{"branch_filters":[{"slot":5,"scope":"repo","remove":true}]}`); code != 200 {
		t.Errorf("remove → %d", code)
	}
	got = bfGet(t, ts)
	if got.BranchFilters[4].Usable || got.BranchFilters[4].Scope != "" {
		t.Errorf("slot 5 still defined after remove: %+v", got.BranchFilters[4])
	}
	// Removing in the repo scope never touches a global definition.
	if code := post(`{"branch_filters":[{"slot":3,"scope":"global","prefix":"g/"}]}`); code != 200 {
		t.Fatalf("global write → %d", code)
	}
	if code := post(`{"branch_filters":[{"slot":3,"scope":"repo","remove":true}]}`); code != 200 {
		t.Fatalf("repo remove of an absent block must be a 200 no-op, got %d", code)
	}
	got = bfGet(t, ts)
	if !got.BranchFilters[2].Usable || got.BranchFilters[2].Prefix != "g/" || got.BranchFilters[2].Scope != "global" {
		t.Errorf("global slot 3 was removed by a repo-scope remove: %+v", got.BranchFilters[2])
	}
	if code := post(`{"branch_filters":[{"slot":1,"scope":"elsewhere","prefix":"x"}]}`); code != 400 {
		t.Errorf("bad scope → %d", code)
	}
	if code := post(`{"branch_filters":[{"slot":6,"scope":"repo","prefix":"x"}]}`); code != 400 {
		t.Errorf("out-of-range slot → %d", code)
	}
}

// TestSettingsBranchFilterUnnamedRoundTrip pins the wire split the edit form
// depends on: the payload's name is the RAW clause (empty here) and label is
// the "slot N" display fallback, so opening an unnamed slot and saving it
// untouched must not bake the label into the file as a name.
func TestSettingsBranchFilterUnnamedRoundTrip(t *testing.T) {
	ts, dir := bfServer(t)
	post := func(body string) int {
		t.Helper()
		return postJSON(t, ts, "/api/settings", body, "application/json", "", nil)
	}
	if code := post(`{"branch_filters":[{"slot":3,"scope":"repo","prefix":"nameless/"}]}`); code != 200 {
		t.Fatalf("write → %d", code)
	}
	got := bfGet(t, ts)
	if got.BranchFilters[2].Name != "" || got.BranchFilters[2].Label != "slot 3" {
		t.Fatalf("unnamed slot 3 = %+v; want name \"\" with label \"slot 3\"", got.BranchFilters[2])
	}
	// Save it back exactly as the form would: name from the wire's raw field.
	body := `{"branch_filters":[{"slot":3,"scope":"repo","name":"` + got.BranchFilters[2].Name +
		`","mode":"` + got.BranchFilters[2].Mode + `","prefix":"` + got.BranchFilters[2].Prefix + `"}]}`
	if code := post(body); code != 200 {
		t.Fatalf("round-trip → %d", code)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".gg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `name = "slot 3"`) {
		t.Errorf("the display label was written back as a name:\n%s", raw)
	}
	got = bfGet(t, ts)
	if got.BranchFilters[2].Name != "" || got.BranchFilters[2].Label != "slot 3" || got.BranchFilters[2].Prefix != "nameless/" {
		t.Errorf("after the round trip: %+v", got.BranchFilters[2])
	}
}

// TestSettingsBranchFilterPeelsOneLayer proves the "both" case the remove
// button peels: a slot defined in BOTH files loses only the repo block, and
// the global one keeps filtering until it is removed on its own.
func TestSettingsBranchFilterPeelsOneLayer(t *testing.T) {
	ts, _ := bfServer(t)
	post := func(body string) int {
		t.Helper()
		return postJSON(t, ts, "/api/settings", body, "application/json", "", nil)
	}
	// Slot 1 is already a repo block ("feat"); add a global one for it.
	if code := post(`{"branch_filters":[{"slot":1,"scope":"global","name":"global feat","prefix":"g/"}]}`); code != 200 {
		t.Fatalf("global write → %d", code)
	}
	got := bfGet(t, ts)
	if got.BranchFilters[0].Scope != "both" || got.BranchFilters[0].Prefix != "feat/" {
		t.Fatalf("slot 1 = %+v; want scope=both with the repo block winning", got.BranchFilters[0])
	}
	if code := post(`{"branch_filters":[{"slot":1,"scope":"repo","remove":true}]}`); code != 200 {
		t.Fatalf("peel repo → %d", code)
	}
	got = bfGet(t, ts)
	if got.BranchFilters[0].Scope != "global" || got.BranchFilters[0].Prefix != "g/" {
		t.Fatalf("after the peel: %+v; want the global block still in force", got.BranchFilters[0])
	}
	if code := post(`{"branch_filters":[{"slot":1,"scope":"global","remove":true}]}`); code != 200 {
		t.Fatalf("peel global → %d", code)
	}
	got = bfGet(t, ts)
	if got.BranchFilters[0].Usable || got.BranchFilters[0].Scope != "" {
		t.Errorf("slot 1 after both peels: %+v", got.BranchFilters[0])
	}
}
