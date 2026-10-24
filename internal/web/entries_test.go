package web

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Bookmark and shelf rows as the TUI switchers draw them: a kind letter,
// the store's display (+ the label), a path cut like a file path.
func TestEntryRowsJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	var mod strings.Builder
	for _, fn := range []string{"runes", "splitPathSegs", "elideNameMiddle", "elidePath"} {
		mod.WriteString(jsFunc(t, "core.js", fn) + "\n")
	}
	for _, fn := range []string{"entryKind", "entryRowText", "entryShown"} {
		mod.WriteString(jsFunc(t, "sidebar.js", fn) + "\n")
	}
	mod.WriteString(`
const bf = { display: "main / abc1234 / src/deep/dir/file.go", path: "src/deep/dir/file.go", is_commit: false };
const bc = { display: "main / abc1234", label: "Add parser", is_commit: true, commit: "abc1234" };
const sf = { kind: "file", display: "wt:repo / working / a/b.go", label: "keep", path: "a/b.go" };
const sc = { kind: "commit", display: "shelf / abc1234", label: "WIP" };
const ss = { kind: "files", display: "wt:repo / working", label: "3 files" };
console.log(JSON.stringify({
  kinds: [entryKind(bf, "bookmarks"), entryKind(bc, "bookmarks"), entryKind(sf, "shelf"), entryKind(sc, "shelf"), entryKind(ss, "shelf")],
  texts: [entryRowText(bf, "bookmarks"), entryRowText(bc, "bookmarks"), entryRowText(sf, "shelf"), entryRowText(sc, "shelf"), entryRowText(ss, "shelf")],
  cut: entryShown(bf, "bookmarks", 24),
  whole: entryShown(bf, "bookmarks", 200),
  commit: entryShown(bc, "bookmarks", 10),
}));
`)
	cmd := exec.Command(node, "--input-type=module", "-e", mod.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Kinds, Texts []string
		Cut, Whole   string
		Commit       string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.Join(got.Kinds, "") != "FCFCS" {
		t.Errorf("kinds = %v, want F C F C S", got.Kinds)
	}
	want := []string{"main / abc1234 / src/deep/dir/file.go", "main / abc1234 — Add parser", "wt:repo / working / a/b.go — keep", "shelf / abc1234 — WIP", "wt:repo / working — 3 files"}
	if strings.Join(got.Texts, "|") != strings.Join(want, "|") {
		t.Errorf("texts =\n%v\nwant\n%v", got.Texts, want)
	}
	if !strings.HasSuffix(got.Cut, "/file.go") || !strings.Contains(got.Cut, "…") || len([]rune(got.Cut)) > 24 {
		t.Errorf("cut = %q: the file name is kept, the middle goes", got.Cut)
	}
	if got.Whole != want[0] {
		t.Errorf("a row that fits stays whole: %q", got.Whole)
	}
	if got.Commit != want[1] {
		t.Errorf("a commit row is not path-cut (CSS ends it): %q", got.Commit)
	}
}

// The menus are the TUI switchers' actions, in their order and words.
func TestEntryMenusWired(t *testing.T) {
	t.Parallel()
	sb := readStaticSrc(t, "sidebar.js")
	for _, want := range []string{
		`"diff against the working-tree file"`,
		`"compare against the selected commit"`,
		`"no commit selected to compare against"`,
		`"select a different commit to compare against"`,
		`"paste to a path…"`,
		`"restore to a path…"`,
		`"copy to a new dir under <repo>.tmp…"`,
		`"copy path…"`,
		`"cherry-pick onto the current branch"`,
		`<span class="ekind">${entryKind(e, "bookmarks")}</span>`,
		`<span class="ekind">${entryKind(e, "shelf")}</span>`,
		"entryPrimary(", // a left click runs the menu's first row (the TUI's enter)
		"renderBookmarks();\n    renderShelf();", // rows re-elide on a sidebar resize
		"setTimeout(() => showCtxMenu(entryCopyRows(e), x, y), 0);", // the picking menu's own close must not take the next one
		"applySection(name, !isCollapsed(name));\n  // A list drawn while folded had no width to cut its paths to.\n  if (name === \"bookmarks\") renderBookmarks();",
	} {
		if !strings.Contains(sb, want) {
			t.Errorf("sidebar.js lacks %s", want)
		}
	}
	bm := sb[strings.Index(sb, "function showBookmarkMenu("):]
	bm = bm[:strings.Index(bm, "\n}\n")]
	if strings.Contains(bm, "...entryCopyRows(e)") {
		t.Error("the bookmark menu's copy rows are one copy path… row now (the TUI's y)")
	}
}
