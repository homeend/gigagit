package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// previewLinkFor builds the LOCAL-form preview link for dir. The local form is
// deliberate: resolveLinkArg reads the package global RepoStatePath, which is
// "" in tests (an empty registry), so the cwd is the only candidate.
func previewLinkFor(dir, path string) string {
	l := "gg://" + filepath.ToSlash(dir)
	if path != "" {
		l += "/" + path
	}
	return l + "@main...feat/x"
}

// advanceSourceBranch adds a SECOND commit to feat/x that touches a
// DIFFERENT file (b.txt) than previewRepo's first commit (a.txt).
//
// Without this, previewRepo's source branch is a single commit, so the
// preview's patch (merge-base..tip) and the tip's own commit diff
// (parent..tip) are the IDENTICAL range by construction — a test comparing
// the two would pass even if the preview-link code path were bypassed
// entirely and fell back to the tip's own commit diff / a plain
// NotesAt(tip) lookup. After this call:
//   - a.txt's own change lives ONLY in the first commit, so it is present in
//     the preview's patch (which spans both commits) but ABSENT from the
//     tip's own commit diff (parent..tip is the second commit alone) — a
//     hunk anchored on a.txt can only resolve through the preview's patch.
//   - a note stored on a.txt while the tip was still the first commit lives
//     on a commit that is no longer the tip, so it is visible only through
//     the preview's along-the-branch note gather (PreviewNotesAt), never
//     through a plain NotesAt(current-tip) lookup.
func advanceSourceBranch(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "checkout", "-q", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "add b")
	gitRun(t, dir, "checkout", "-q", "main")
}

// A table over every §2.3 consumer: the preview link and --preview must
// produce exactly the same bytes / the same stored note (ruling 5).
func TestPreviewLinkMatchesThePreviewFlag(t *testing.T) {
	dir := previewRepo(t)
	fileLink := previewLinkFor(dir, "a.txt")
	// T4a: previewRepo seeds no notes, so the "note list" row below would be
	// a vacuous comparison (two empty lists always match). Seed one note via
	// --preview FIRST — while the source tip is still the commit that adds
	// a.txt, so the note lands on it — and only THEN advance the source
	// branch (fix round 1: see advanceSourceBranch's doc comment). This
	// makes every row below discriminating: a bypassed link would show a
	// smaller diff (missing a.txt's own commit) and an empty note list
	// (the note now lives on a non-tip commit), not merely an untested-but-
	// coincidentally-equal one.
	if code, _, errb := runCLI(t, dir, "note", "add", "--preview", "main...feat/x", "--file", "a.txt", "--new-line", "1", "--summary", "seeded preview note"); code != 0 {
		t.Fatalf("seed note: %d %s", code, errb)
	}
	advanceSourceBranch(t, dir)
	cases := []struct {
		name     string
		flagArgs []string
		linkArgs []string
	}{
		// -- a.txt scopes the flag path to the same single file the link
		// path is already scoped to (fileLink carries /a.txt) — otherwise,
		// after advanceSourceBranch, the flag's UNSCOPED preview diff would
		// legitimately include b.txt while the link's diff would not, and
		// the two would disagree for a reason that has nothing to do with
		// the adapter.
		{"diff", []string{"diff", "--preview", "main...feat/x", "--", "a.txt"}, []string{"diff", fileLink}},
		{"diff --hunks --json", []string{"diff", "--preview", "main...feat/x", "--hunks", "--json", "--", "a.txt"}, []string{"diff", fileLink, "--hunks", "--json"}},
		{"note list", []string{"note", "list", "--preview", "main...feat/x", "--file", "a.txt"}, []string{"note", "list", fileLink}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			codeF, outF, errF := runCLI(t, dir, c.flagArgs...)
			codeL, outL, errL := runCLI(t, dir, c.linkArgs...)
			if codeF != 0 || codeL != 0 {
				t.Fatalf("exit codes = %d (flag: %s) / %d (link: %s)", codeF, errF, codeL, errL)
			}
			if outF != outL {
				t.Errorf("--preview and the link disagree:\nflag: %q\nlink: %q", outF, outL)
			}
			if c.name == "note list" {
				// T4a: both outputs must carry the seeded note (same status
				// word, since the lines above already proved byte-identity).
				if !strings.Contains(outF, "seeded preview note") || !strings.Contains(outL, "seeded preview note") {
					t.Fatalf("seeded note missing:\nflag: %q\nlink: %q", outF, outL)
				}
			}
		})
	}
}

// note add through a preview link stores on the tip, numbered over the
// PREVIEW's patch — exactly as --preview --hunk N does.
func TestNoteAddThroughAPreviewLinkStoresOnTheTip(t *testing.T) {
	dir := previewRepo(t)
	// Fix round 1: advance the source branch so a.txt's hunk exists ONLY in
	// the preview's patch, not in the tip's own commit diff (see
	// advanceSourceBranch). Without this, #1 would resolve identically
	// whether or not previewTargetFromLink's adapter ran.
	advanceSourceBranch(t, dir)
	if code, _, errb := runCLI(t, dir, "note", "add", previewLinkFor(dir, "a.txt")+"#1", "--summary", "linked preview note"); code != 0 {
		t.Fatalf("note add: %d %s", code, errb)
	}
	// The same note must be visible on the source tip's own commit view.
	tip := strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if !strings.Contains(out, "linked preview note") {
		t.Fatalf("the note is not on the tip %s: %q", tip, out)
	}
}

// A preview link and --preview together is a usage error, never a silent
// override (ruling 5).
func TestPreviewLinkPlusPreviewFlagIsAUsageError(t *testing.T) {
	dir := previewRepo(t)
	for _, args := range [][]string{
		{"note", "add", previewLinkFor(dir, "a.txt") + ":1", "--preview", "main...feat/x", "--summary", "no"},
		{"note", "list", previewLinkFor(dir, "a.txt"), "--preview", "main...feat/x"},
		// A bare preview link is a valid apply target (noteLinkShape's
		// "apply" case refuses a PATH, not a bare link) — --preview on top
		// of it is still the usual link-plus-flag conflict.
		{"note", "apply", previewLinkFor(dir, ""), "--preview", "main...feat/x", "--stdin"},
		{"diff", previewLinkFor(dir, "a.txt"), "--preview", "main...feat/x"},
	} {
		if code, _, errb := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v: exit = %d, want 2 (%s)", args, code, errb)
		}
	}
}

// Fix round 2: spec §2.3 treats a preview link exactly as --preview,
// including WITHOUT a path. `gg note list --preview P` with no --file lists
// every path via PreviewNotesAll, so a BARE preview link (no path) must be
// accepted by `note list` and take the same path-less route, byte for byte
// with the flag — in both the text and --json forms. `note add` still
// refuses a bare link: a note needs a file even when the link is a preview.
func TestNoteListAcceptsABarePreviewLink(t *testing.T) {
	dir := previewRepo(t)
	bareLink := previewLinkFor(dir, "")
	if code, _, errb := runCLI(t, dir, "note", "add", "--preview", "main...feat/x", "--file", "a.txt", "--new-line", "1", "--summary", "seeded preview note"); code != 0 {
		t.Fatalf("seed note: %d %s", code, errb)
	}
	for _, withJSON := range []bool{false, true} {
		flagArgs := []string{"note", "list", "--preview", "main...feat/x"}
		linkArgs := []string{"note", "list", bareLink}
		if withJSON {
			flagArgs = append(flagArgs, "--json")
			linkArgs = append(linkArgs, "--json")
		}
		codeF, outF, errF := runCLI(t, dir, flagArgs...)
		codeL, outL, errL := runCLI(t, dir, linkArgs...)
		if codeF != 0 || codeL != 0 {
			t.Fatalf("json=%v exit codes = %d (flag: %s) / %d (link: %s)", withJSON, codeF, errF, codeL, errL)
		}
		if outF != outL {
			t.Errorf("json=%v --preview and the bare link disagree:\nflag: %q\nlink: %q", withJSON, outF, outL)
		}
		if !strings.Contains(outF, "seeded preview note") {
			t.Fatalf("json=%v seeded note missing: %q", withJSON, outF)
		}
	}
	if code, _, errb := runCLI(t, dir, "note", "add", bareLink, "--summary", "no file"); code != 2 {
		t.Fatalf("note add <bare preview link>: exit = %d, want 2 (%s)", code, errb)
	}
}

func TestShowRefusesAPreviewLink(t *testing.T) {
	dir := previewRepo(t)
	code, _, errb := runCLI(t, dir, "show", previewLinkFor(dir, "a.txt"))
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb, "gg diff") {
		t.Errorf("stderr = %q, want it to point at gg diff", errb)
	}
}

// note apply through a preview link imports onto the tip, new side only.
func TestNoteApplyThroughAPreviewLinkImportsOntoTheTip(t *testing.T) {
	dir := previewRepo(t)
	// Fix round 1: advance the source branch (see advanceSourceBranch) and
	// anchor the batch item by HUNK, not newRange. A newRange item never
	// touches NoteBatchTarget.Hunks — the ONE field that distinguishes the
	// preview's patch from the tip's own commit diff — so it could pass
	// unchanged whether or not the preview arm in noteapply.go ran. The
	// "comment apply" shape's hunk field (notebatch's rawComment) is what
	// routes through planNoteBatchAnchor's t.Hunk != 0 case, which consults
	// bt.Hunks.
	advanceSourceBranch(t, dir)
	batch := `{"comments":[{"filePath":"a.txt","hunk":1,"summary":"batched"}]}`
	code, _, errb := runCLIStdin(t, dir, batch, "note", "apply", previewLinkFor(dir, ""), "--stdin")
	if code != 0 {
		t.Fatalf("note apply: %d %s", code, errb)
	}
	tip := strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if !strings.Contains(out, "batched") {
		t.Fatalf("the batch did not land on the tip: %q", out)
	}
}

// note clear with a preview FILE link clears the tip's notes for that path;
// a BARE preview link is refused by noteLinkShape exactly as a bare commit
// link is (a repository link cannot carry a target here).
//
// Fix round 1: this test does NOT need advanceSourceBranch / a hunk anchor.
// noteClear never calls previewTargetFromLink — it clears by link.Addr (the
// FileAddress {Committed, tip, path} Task 2/3 already resolve), exactly the
// same address a plain commit link would carry. There is no adapter branch
// here to bypass, so the single-commit previewRepo fixture is not vacuous
// for what this test actually exercises (Addr resolving to the tip).
func TestNoteClearWithPreviewLinks(t *testing.T) {
	dir := previewRepo(t)
	if code, _, errb := runCLI(t, dir, "note", "add", previewLinkFor(dir, "a.txt")+":1", "--summary", "to clear"); code != 0 {
		t.Fatalf("note add: %d %s", code, errb)
	}
	// --yes is mandatory (note.go:753); a FILE link stands in for --file, and
	// noteClear then clears link.Addr — the tip + path (note.go:734-762).
	if code, _, errb := runCLI(t, dir, "note", "clear", previewLinkFor(dir, "a.txt"), "--yes"); code != 0 {
		t.Fatalf("note clear: %d %s", code, errb)
	}
	tip := strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if strings.Contains(out, "to clear") {
		t.Fatalf("the note survived the clear: %q", out)
	}
	if code, _, _ := runCLI(t, dir, "note", "clear", previewLinkFor(dir, ""), "--all", "--yes"); code != 2 {
		t.Error("a BARE preview link carries a target: note clear must refuse it (exit 2)")
	}
}

// gg link resolve prints the pair for a preview link, in both forms.
func TestLinkResolvePrintsThePreviewPair(t *testing.T) {
	dir := previewRepo(t)
	_, out, errb := runCLI(t, dir, "link", "resolve", previewLinkFor(dir, "a.txt")+":2")
	if !strings.Contains(out, "preview main...feat/x") || !strings.Contains(out, "a.txt") {
		t.Fatalf("resolve = %q (%s)", out, errb)
	}
	_, jsonOut, _ := runCLI(t, dir, "link", "resolve", "--json", previewLinkFor(dir, "a.txt")+":2")
	var w struct {
		State  string `json:"state"`
		Source string `json:"source"`
		Target string `json:"target"`
		Commit string `json:"commit"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &w); err != nil {
		t.Fatalf("json: %v (%q)", err, jsonOut)
	}
	if w.Source != "feat/x" || w.Target != "main" {
		t.Errorf("json pair = %s...%s", w.Target, w.Source)
	}
	if w.Commit == "" || w.Path != "a.txt" {
		t.Errorf("json = %+v, want the tip sha and the path", w)
	}
}
