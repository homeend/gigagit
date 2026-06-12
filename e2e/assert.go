package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// assertExpect verifies every expectation and reports each mismatch.
func assertExpect(t *testing.T, sb *Sandbox, exp *Expect) {
	t.Helper()
	for _, f := range checkExpect(sb, exp) {
		t.Error(f)
	}
}

// checkExpect returns one human-readable line per failed expectation.
// Queries go straight to git (plumbing-ish), independent of internal/git,
// so the assertions cannot share a bug with the code under test.
func checkExpect(sb *Sandbox, exp *Expect) (fails []string) {
	dir := sb.LocalDir
	addf := func(format string, a ...any) { fails = append(fails, fmt.Sprintf(format, a...)) }

	if exp.Branch != "" {
		got, err := gitOut(dir, "branch", "--show-current")
		if err != nil {
			addf("current branch: %v", err)
		} else if got != exp.Branch {
			addf("current branch: want %q, got %q", exp.Branch, got)
		}
	}
	if exp.Branches != nil {
		got, err := gitLines(dir, "for-each-ref", "refs/heads", "--format=%(refname:short)")
		if err != nil {
			addf("branches: %v", err)
		} else if !sameSet(got, exp.Branches) {
			addf("branches: want %v, got %v", sorted(exp.Branches), sorted(got))
		}
	}

	st, err := readStatus(dir)
	if err != nil {
		addf("status: %v", err)
	} else {
		if exp.Clean != nil && *exp.Clean && !st.clean() {
			addf("clean: working tree is dirty: staged=%v unstaged=%v untracked=%v conflicted=%v",
				st.staged, st.unstaged, st.untracked, st.conflicted)
		}
		checkStatus(&fails, "", exp.Status, st)
	}

	checkFiles(&fails, "", dir, exp.FilesN, sb.InputSums)

	if exp.Stashes != nil || len(exp.Stash) > 0 {
		entries, err := gitLines(dir, "stash", "list", "--format=%gd")
		if err != nil {
			addf("stash list: %v", err)
		} else {
			if exp.Stashes != nil && len(entries) != *exp.Stashes {
				addf("stash count: want %d, got %d", *exp.Stashes, len(entries))
			}
			for i, se := range exp.Stash {
				if i >= len(entries) {
					addf("stash[%d]: want an entry, only %d stashes exist", i, len(entries))
					continue
				}
				for _, path := range sortedKeys(se.Contains) {
					want := se.Contains[path]
					got, ok := stashFile(dir, i, path)
					if !ok {
						addf("stash[%d]: want it to contain %q, file not in stash", i, path)
					} else if got != want {
						addf("stash[%d] %q: want %q, got %q", i, path, want, got)
					}
				}
			}
		}
	}

	return fails
}

// checkFiles verifies one scope's file expectations (scope "" = main repo).
func checkFiles(fails *[]string, scope, dir string, files map[string]FileExpect, inputSums map[string]string) {
	addf := func(format string, a ...any) { *fails = append(*fails, fmt.Sprintf(format, a...)) }
	prefix := ""
	if scope != "" {
		prefix = "worktree " + scope + ": "
	}
	for _, path := range sortedKeys(files) {
		fe := files[path]
		full := filepath.Join(dir, path)
		if fe.Absent {
			if _, err := os.Stat(full); err == nil {
				addf("%sfile %q: want absent, exists", prefix, path)
			}
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			addf("%sfile %q: want present, %v", prefix, path, err)
			continue
		}
		switch {
		case fe.HasContent:
			if string(data) != fe.Content {
				addf("%sfile %q: want %q, got %q", prefix, path, fe.Content, string(data))
			}
		case fe.SHA256 != "":
			sum, _ := fileSHA256(full)
			if sum != fe.SHA256 {
				addf("%sfile %q: want sha256 %s, got %s", prefix, path, fe.SHA256, sum)
			}
		case fe.Unchanged:
			sum, _ := fileSHA256(full)
			ref, ok := inputSums[filepath.ToSlash(path)]
			if !ok {
				addf("%sfile %q: unchanged asserted but file was not in the input snapshot", prefix, path)
			} else if sum != ref {
				addf("%sfile %q: want unchanged from input, content differs", prefix, path)
			}
		}
	}
}

// checkStatus verifies one scope's status expectation against a parsed status.
func checkStatus(fails *[]string, scope string, want *StatusExpect, st *repoStatus) {
	if want == nil {
		return
	}
	prefix := ""
	if scope != "" {
		prefix = "worktree " + scope + ": "
	}
	cmp := func(name string, w, g []string) {
		if w != nil && !sameSet(w, g) {
			*fails = append(*fails, fmt.Sprintf("%sstatus %s: want %v, got %v", prefix, name, sorted(w), sorted(g)))
		}
	}
	cmp("staged", want.Staged, st.staged)
	cmp("unstaged", want.Unstaged, st.unstaged)
	cmp("untracked", want.Untracked, st.untracked)
	cmp("conflicted", want.Conflicted, st.conflicted)
}

// repoStatus is `git status --porcelain` parsed into the four user-visible sets.
type repoStatus struct {
	staged, unstaged, untracked, conflicted []string
}

func (s *repoStatus) clean() bool {
	return len(s.staged)+len(s.unstaged)+len(s.untracked)+len(s.conflicted) == 0
}

var conflictCodes = map[string]bool{
	"DD": true, "AU": true, "UD": true, "UA": true, "DU": true, "AA": true, "UU": true,
}

func readStatus(dir string) (*repoStatus, error) {
	lines, err := gitLines(dir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	st := &repoStatus{}
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		xy, path := line[:2], line[3:]
		if i := strings.Index(path, " -> "); i >= 0 { // rename: use the new path
			// Note: if old/new names contain spaces git may C-quote the field;
			// scenario filenames must avoid spaces to stay unambiguous here.
			path = path[i+4:]
		}
		switch {
		case xy == "??":
			st.untracked = append(st.untracked, path)
		case conflictCodes[xy]:
			st.conflicted = append(st.conflicted, path)
		default:
			if xy[0] != ' ' {
				st.staged = append(st.staged, path)
			}
			if xy[1] != ' ' {
				st.unstaged = append(st.unstaged, path)
			}
		}
	}
	return st, nil
}

// stashFile reads path from stash entry n: the stash commit's tree holds
// tracked changes; untracked files live in the third parent (^3). A missing
// ^3 parent is a benign error path (the stash had no untracked files) and
// returns ok=false.
func stashFile(dir string, n int, path string) (string, bool) {
	ref := fmt.Sprintf("stash@{%d}", n)
	if out, err := gitRaw(dir, "show", ref+":"+path); err == nil {
		return out, true
	}
	if out, err := gitRaw(dir, "show", ref+"^3:"+path); err == nil {
		return out, true
	}
	return "", false
}

// gitOut runs git and returns trimmed stdout (assertion queries: real clock).
func gitOut(dir string, args ...string) (string, error) {
	out, err := gitRaw(dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// gitRaw is gitOut without newline trimming (exact blob content).
func gitRaw(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %v: %w", args, err)
	}
	return string(out), nil
}

func gitLines(dir string, args ...string) ([]string, error) {
	out, err := gitOut(dir, args...)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

func sameSet(a, b []string) bool {
	as, bs := sorted(a), sorted(b)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
