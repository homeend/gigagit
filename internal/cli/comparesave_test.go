package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every test here sets XDG_STATE_HOME via t.Setenv to isolate the
// saved-comparison store, which forbids t.Parallel. SERIAL throughout —
// the same convention as compare_entries_test.go and linkhint_test.go.

// saveRepo is a repo with two commits, and an isolated state dir.
func saveRepo(t *testing.T) (dir, head, parent string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir = newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "c2")
	return dir, strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD")),
		strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~1"))
}

// --save stores BOTH sides as links and prints the id. The @worktree side is
// the one that proves the mapping: it has no commit, so a token→link map that
// fell through to the commit-ish arm would resolve it as a revision.
func TestCompareSaveStoresBothSidesAsLinks(t *testing.T) {
	dir, head, _ := saveRepo(t)

	code, out, errb := runCLI(t, dir, "compare", "--save", "my comparison", head, "@worktree")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb)
	}
	// STDOUT stays the changed-file list, parseable by `cut`: every line has
	// the two columns printCompareFiles emits, and no id row among them.
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if ln == "" {
			continue
		}
		if n := len(strings.Split(ln, "\t")); n != 2 {
			t.Fatalf("stdout line %q has %d columns, want the 2 of a file list", ln, n)
		}
	}

	lcode, lout, lerr := runCLI(t, dir, "compare", "--list")
	if lcode != 0 {
		t.Fatalf("--list exit %d, stderr: %s", lcode, lerr)
	}
	fields := strings.Split(strings.TrimRight(lout, "\n"), "\t")
	if len(fields) != 4 {
		t.Fatalf("--list line has %d fields, want 4 (id, label, left, right):\n%q", len(fields), lout)
	}
	id, label, left, right := fields[0], fields[1], fields[2], fields[3]
	if label != "my comparison" {
		t.Fatalf("label = %q", label)
	}
	if !strings.HasPrefix(left, "gg://") || !strings.HasPrefix(right, "gg://") {
		t.Fatalf("sides are not links: left=%q right=%q", left, right)
	}
	// The left side pinned a commit; the right side is the working tree and
	// must carry NO target at all.
	if !strings.Contains(left, "@"+head) {
		t.Fatalf("left = %q, want the commit %s", left, head)
	}
	if strings.Contains(right, "@") {
		t.Fatalf("@worktree mapped to a pinned target: %q", right)
	}
	if strings.Contains(out, id) {
		t.Fatalf("the id leaked into stdout, which must stay a parseable file list: %q", out)
	}
	if !strings.Contains(errb, id) {
		t.Fatalf("stderr did not report the saved id %q: %q", id, errb)
	}
}

// --list prints NOTHING at exit 0 on an empty store — the convention
// `gg links` already follows, so a caller can pipe it without a header.
func TestCompareListIsEmptyAndSilentWithNothingStored(t *testing.T) {
	dir, _, _ := saveRepo(t)
	code, out, errb := runCLI(t, dir, "compare", "--list")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb)
	}
	if out != "" {
		t.Fatalf("--list printed %q on an empty store, want nothing", out)
	}
}

// --saved re-runs a stored comparison by id AND by label, printing exactly
// what the original invocation printed.
func TestCompareSavedReRunsByIDAndLabel(t *testing.T) {
	dir, head, parent := saveRepo(t)
	// An UNCOMMITTED file, so the working tree differs from HEAD. Without it
	// `parent..HEAD` and `parent..@worktree` print the same thing, and a
	// --saved that silently dropped the stored right half would be invisible:
	// the assertion could not see its own subject.
	os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirty\n"), 0o644)

	_, want, _ := runCLI(t, dir, "compare", parent, head)
	if want == "" {
		t.Fatal("the direct comparison printed nothing; fixture is useless")
	}
	_, wtOut, _ := runCLI(t, dir, "compare", parent, "@worktree")
	if wtOut == want {
		t.Fatalf("fixture is useless: comparing against @worktree prints the same as against HEAD:\n%s", want)
	}
	if code, _, errb := runCLI(t, dir, "compare", "--save", "mine", parent, head); code != 0 {
		t.Fatalf("save: exit %d, stderr: %s", code, errb)
	}
	_, lout, _ := runCLI(t, dir, "compare", "--list")
	id := strings.SplitN(strings.TrimRight(lout, "\n"), "\t", 2)[0]

	for _, spec := range []string{"mine", id} {
		code, got, errb := runCLI(t, dir, "compare", "--saved", spec)
		if code != 0 {
			t.Fatalf("--saved %q: exit %d, stderr: %s", spec, code, errb)
		}
		if got != want {
			t.Fatalf("--saved %q printed:\n%s\nwant the original:\n%s", spec, got, want)
		}
	}
}

// A token that cannot be expressed as a link is refused at exit 2 rather than
// stored as something else — and nothing is written.
func TestCompareSaveRefusesATokenItCannotLink(t *testing.T) {
	dir, _, _ := saveRepo(t)
	code, _, errb := runCLI(t, dir, "compare", "--save", "x", "bookmark:nosuchid", "@worktree")
	if code != 2 {
		t.Fatalf("exit %d, want 2; stderr: %s", code, errb)
	}
	_, lout, _ := runCLI(t, dir, "compare", "--list")
	if lout != "" {
		t.Fatalf("a refused --save stored a row: %q", lout)
	}
}

// --save runs only on SUCCESS: a comparison that failed is not one the user
// asked to keep (ruling R8, the rule recordCompareLinks already follows).
func TestCompareSaveStoresNothingWhenTheComparisonFails(t *testing.T) {
	dir, _, _ := saveRepo(t)
	code, _, _ := runCLI(t, dir, "compare", "--save", "x", "nosuchrev", "@worktree")
	if code == 0 {
		t.Fatal("compare with an unknown rev exited 0")
	}
	_, lout, _ := runCLI(t, dir, "compare", "--list")
	if lout != "" {
		t.Fatalf("a failed comparison stored a row: %q", lout)
	}
}

// --save and --saved together is a usage error: one writes, the other reads.
func TestCompareSaveAndSavedTogetherIsUsage(t *testing.T) {
	dir, _, _ := saveRepo(t)
	if code, _, _ := runCLI(t, dir, "compare", "--save", "x", "--saved", "y"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

// An unknown --saved spec is a usage error naming the spec, not a 0-exit
// no-op and not a crash.
func TestCompareSavedUnknownSpecIsUsage(t *testing.T) {
	dir, _, _ := saveRepo(t)
	code, _, errb := runCLI(t, dir, "compare", "--saved", "nosuchthing")
	if code != 2 {
		t.Fatalf("exit %d, want 2; stderr: %s", code, errb)
	}
	if !strings.Contains(errb, "nosuchthing") {
		t.Fatalf("stderr does not name the spec: %s", errb)
	}
}

// twoSaved stores two comparisons and returns their ids in save order.
func twoSaved(t *testing.T) (dir, firstID, secondID string) {
	t.Helper()
	dir, head, parent := saveRepo(t)
	for _, a := range [][]string{
		{"compare", "--save", "first", parent, head},
		{"compare", "--save", "second", head, "@worktree"},
	} {
		if code, _, errb := runCLI(t, dir, a...); code != 0 {
			t.Fatalf("%v: exit %d, stderr: %s", a, code, errb)
		}
	}
	_, lout, _ := runCLI(t, dir, "compare", "--list")
	ids := map[string]string{}
	for _, ln := range strings.Split(strings.TrimRight(lout, "\n"), "\n") {
		f := strings.Split(ln, "\t")
		ids[f[1]] = f[0]
	}
	if ids["first"] == "" || ids["second"] == "" || ids["first"] == ids["second"] {
		t.Fatalf("fixture: list = %q", lout)
	}
	return dir, ids["first"], ids["second"]
}

// TWO rows, and the one removed is the SECOND, by label: a remove that deleted
// everything, or the first row, or matched loosely, cannot pass.
func TestCompareRemoveDeletesExactlyTheNamedRow(t *testing.T) {
	dir, firstID, secondID := twoSaved(t)

	code, out, errb := runCLI(t, dir, "compare", "--remove", "second")
	if code != 0 || out != "" {
		t.Fatalf("exit %d, stdout %q (must be empty), stderr %q", code, out, errb)
	}
	if !strings.Contains(errb, "# removed: "+secondID+"\tsecond") {
		t.Errorf("stderr = %q, want the removed id and label", errb)
	}
	_, lout, _ := runCLI(t, dir, "compare", "--list")
	if !strings.Contains(lout, firstID+"\tfirst") || strings.Contains(lout, "second") {
		t.Fatalf("list after remove = %q, want exactly the first row", lout)
	}

	// …and by ID.
	if code, _, errb := runCLI(t, dir, "compare", "--remove", firstID); code != 0 {
		t.Fatalf("remove by id: exit %d, stderr %q", code, errb)
	}
	if _, lout, _ := runCLI(t, dir, "compare", "--list"); lout != "" {
		t.Fatalf("list = %q, want empty", lout)
	}
}

func TestCompareRenameKeepsTheIDAndLeavesTheOtherRowAlone(t *testing.T) {
	dir, firstID, secondID := twoSaved(t)
	code, out, errb := runCLI(t, dir, "compare", "--rename", "second", "a better name")
	if code != 0 || out != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
	}
	if !strings.Contains(errb, "# renamed: "+secondID+"\ta better name") {
		t.Errorf("stderr = %q", errb)
	}
	_, lout, _ := runCLI(t, dir, "compare", "--list")
	if !strings.Contains(lout, secondID+"\ta better name\t") || !strings.Contains(lout, firstID+"\tfirst\t") {
		t.Fatalf("list = %q", lout)
	}
	// The new label now names it; the old one names nothing.
	if code, _, _ := runCLI(t, dir, "compare", "--saved", "a better name"); code != 0 {
		t.Error("--saved by the new label failed")
	}
	if code, _, _ := runCLI(t, dir, "compare", "--saved", "second"); code != 2 {
		t.Errorf("--saved by the OLD label: exit %d, want 2", code)
	}
}

func TestCompareRemoveAndRenameUsageErrors(t *testing.T) {
	dir, _, _ := twoSaved(t)
	for _, a := range [][]string{
		{"compare", "--remove", "nosuch"},
		{"compare", "--rename", "nosuch", "x"},
		{"compare", "--rename", "first"},                // no new label
		{"compare", "--rename", "first", "a", "b"},      // two positionals
		{"compare", "--remove", "first", "HEAD"},        // an endpoint
		{"compare", "--remove", "first", "--list"},      // flag after a positional-less mode
		{"compare", "--list", "--remove", "first"},      //
		{"compare", "--save", "x", "--remove", "first"}, //
		{"compare", "--saved", "first", "--rename", "second", "z"},
		{"compare", "--remove", "first", "--rename", "second", "z"},
	} {
		if code, _, _ := runCLI(t, dir, a...); code != 2 {
			t.Errorf("%v: exit %d, want 2", a, code)
		}
	}
	// None of those touched the store.
	_, lout, _ := runCLI(t, dir, "compare", "--list")
	if strings.Count(lout, "\n") != 2 || !strings.Contains(lout, "\tfirst\t") || !strings.Contains(lout, "\tsecond\t") {
		t.Fatalf("a refused invocation changed the store: %q", lout)
	}
}

// A converted merge preview is a row of the SAME store, so --remove reaches it.
func TestCompareRemoveReachesASavedPreview(t *testing.T) {
	dir, _, _ := saveRepo(t)
	gitRun(t, dir, "branch", "feat/x")
	if code, _, errb := runCLI(t, dir, "preview", "add", "--label", "my preview", "feat/x", "main"); code != 0 {
		t.Fatalf("preview add: exit %d, stderr %q", code, errb)
	}
	if _, pout, _ := runCLI(t, dir, "preview", "list"); !strings.Contains(pout, "my preview") {
		t.Fatalf("fixture: preview list = %q", pout)
	}
	if code, _, errb := runCLI(t, dir, "compare", "--remove", "my preview"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb)
	}
	if _, pout, _ := runCLI(t, dir, "preview", "list"); strings.Contains(pout, "my preview") {
		t.Fatalf("preview list still shows it: %q", pout)
	}
}
