package commitgraph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func trimRows(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.TrimRight(r.Cells, " ")
	}
	return out
}

func assertGraph(t *testing.T, commits []Commit, want []string) {
	t.Helper()
	rows, _ := Lay(commits)
	got := trimRows(rows)
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d\n got=%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q\n full got=%q", i, got[i], want[i], got)
		}
	}
}

func TestLinear(t *testing.T) {
	assertGraph(t, []Commit{
		{"c3", []string{"c2"}},
		{"c2", []string{"c1"}},
		{"c1", nil},
	}, []string{"●", "●", "●"})
}

func TestBranchAndMerge(t *testing.T) {
	assertGraph(t, []Commit{
		{"c5", []string{"c4", "c3"}},
		{"c4", []string{"c2"}},
		{"c3", []string{"c2"}},
		{"c2", []string{"c1"}},
		{"c1", nil},
	}, []string{
		"●─╮",
		"● │",
		"│ ●",
		"●─╯",
		"●",
	})
}

func TestPassThroughAcrossMerge(t *testing.T) {
	assertGraph(t, []Commit{
		{"a", []string{"base"}},
		{"b", []string{"bp"}},
		{"c", []string{"base"}},
		{"base", []string{"root"}},
		{"root", nil},
		{"bp", nil},
	}, []string{
		"●",
		"│ ●",
		"│ │ ●",
		"●─┼─╯",
		"● │",
		"  ●",
	})
}

func TestOctopusMerge(t *testing.T) {
	assertGraph(t, []Commit{
		{"m", []string{"p1", "p2", "p3"}},
		{"p1", nil},
		{"p2", nil},
		{"p3", nil},
	}, []string{
		"●─┬─╮",
		"● │ │",
		"  ● │",
		"    ●",
	})
}

func TestTwoRoots(t *testing.T) {
	assertGraph(t, []Commit{
		{"a2", []string{"a1"}},
		{"b2", []string{"b1"}},
		{"a1", nil},
		{"b1", nil},
	}, []string{
		"●",
		"│ ●",
		"● │",
		"  ●",
	})
}

func TestWidthNormalized(t *testing.T) {
	rows, width := Lay([]Commit{
		{"m", []string{"p1", "p2"}},
		{"p1", []string{"r"}},
		{"p2", []string{"r"}},
		{"r", nil},
	})
	if width != 4 {
		t.Fatalf("width = %d, want 4 (2 lanes)", width)
	}
	for i, r := range rows {
		if len([]rune(r.Cells)) != 4 {
			t.Fatalf("row %d Cells %q has %d runes, want 4 (padded to width)", i, r.Cells, len([]rune(r.Cells)))
		}
	}
}

func buildRealRepoCommits(t *testing.T) []Commit {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	w := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	w("a", "1")
	run("add", "-A")
	run("commit", "-m", "base")
	run("checkout", "-b", "feature")
	w("b", "1")
	run("add", "-A")
	run("commit", "-m", "feat")
	run("checkout", "main")
	w("c", "1")
	run("add", "-A")
	run("commit", "-m", "main work")
	run("merge", "--no-ff", "-m", "merge feature", "feature")
	out, err := exec.Command("git", "-C", dir, "log", "--all", "--date-order", "--format=%H %P").Output()
	if err != nil {
		t.Fatal(err)
	}
	var cs []Commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		cs = append(cs, Commit{Hash: f[0], Parents: f[1:]})
	}
	return cs
}

func TestLayRealRepoInvariants(t *testing.T) {
	cs := buildRealRepoCommits(t)
	rows, _ := Lay(cs)
	if len(rows) != len(cs) {
		t.Fatalf("rows %d != commits %d", len(rows), len(cs))
	}
	idx := map[string]int{}
	for i, c := range cs {
		idx[c.Hash] = i
	}
	for i, r := range rows {
		if strings.Count(r.Cells, "●") != 1 {
			t.Fatalf("row %d %q must have exactly one node", i, r.Cells)
		}
		for _, p := range cs[i].Parents {
			if j, ok := idx[p]; ok && j <= i {
				t.Fatalf("parent %s of row %d must appear later, got row %d", p[:7], i, j)
			}
		}
	}
	multi := false
	for _, r := range rows {
		if strings.ContainsAny(r.Cells, "╮╭╯╰┬┴┼") {
			multi = true
		}
	}
	if !multi {
		t.Fatal("a repo with a merge must produce at least one fork/merge glyph")
	}
}

func TestLayClampsPlaneToMaxLanes(t *testing.T) {
	// One merge commit with far more parents than the ceiling forces a very
	// wide node row; every parent is a distinct root that frees its lane next.
	const parents = 400
	cs := make([]Commit, 0, parents+1)
	m := Commit{Hash: "M"}
	for i := 0; i < parents; i++ {
		p := "p" + strconv.Itoa(i)
		m.Parents = append(m.Parents, p)
	}
	cs = append(cs, m)
	for i := 0; i < parents; i++ {
		cs = append(cs, Commit{Hash: "p" + strconv.Itoa(i)})
	}

	rows, width := Lay(cs)
	if width != MaxLanes*2 {
		t.Fatalf("width = %d, want clamped to MaxLanes*2 = %d", width, MaxLanes*2)
	}
	for i, r := range rows {
		if r.Width != width {
			t.Fatalf("row %d Width = %d, want %d", i, r.Width, width)
		}
		if got := len([]rune(r.Cells)); got != width {
			t.Fatalf("row %d cell runes = %d, want %d", i, got, width)
		}
	}
}
