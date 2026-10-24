package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// claimFixture: a repo with a linked worktree wt2 claimed by "bob" from a
// foreign host (never judged dead, so no live gg is needed) and reserved in
// the repo config.
func claimFixture(t *testing.T) (main, second string) {
	t.Helper()
	main = newRepoDir(t, 1)
	second = filepath.Join(t.TempDir(), "wt2")
	gitRun(t, main, "worktree", "add", "-b", "w2", second)
	claim := "session = \"9-1\"\nagent = \"bob\"\nsince = 2026-10-24T13:05:00Z\nnote = \"fixing the parser\"\nhost = \"elsewhere/plan9\"\n"
	if err := os.WriteFile(filepath.Join(main, ".git", "worktrees", "wt2", "gg-claim"), []byte(claim), 0o644); err != nil {
		t.Fatal(err)
	}
	return main, second
}

func TestWorktreesCarryMarks(t *testing.T) {
	t.Parallel()
	main, second := claimFixture(t)
	if err := os.WriteFile(filepath.Join(main, ".gg.toml"), []byte("[agents]\nreserved = ["+tomlQuote(second)+"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(main)))
	var body struct {
		Worktrees []struct {
			Path     string `json:"path"`
			Reserved bool   `json:"reserved"`
			Claim    *struct {
				Agent string `json:"agent"`
				Since string `json:"since"`
				Note  string `json:"note"`
			} `json:"claim"`
		} `json:"worktrees"`
	}
	if code := getJSON(t, ts, "/api/worktrees", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Worktrees) != 2 {
		t.Fatalf("rows = %+v", body.Worktrees)
	}
	for _, w := range body.Worktrees {
		if domain.SameCheckout(w.Path, main) {
			if w.Reserved || w.Claim != nil {
				t.Errorf("main row marked: %+v", w)
			}
			continue
		}
		if !w.Reserved {
			t.Errorf("wt2 not reserved: %+v", w)
		}
		if w.Claim == nil || w.Claim.Agent != "bob" || w.Claim.Since != "2026-10-24T13:05:00Z" || w.Claim.Note != "fixing the parser" {
			t.Errorf("wt2 claim = %+v", w.Claim)
		}
	}
}

func tomlQuote(s string) string { return "'" + s + "'" }

// The Worktrees row draws the TUI's mark column before the label and the
// TUI bar's text in its tooltip, under the path.
func TestWorktreeRowDrawsMarks(t *testing.T) {
	t.Parallel()
	src := readStaticSrc(t, "sidebar.js")
	for _, want := range []string{
		`import { claimHint, markPrefix } from "./wtmarks.js";`,
		`const marks = markPrefix(w);`,
		"${mark(!!cur)}${marks ? `<span class=\"wmark\">${esc(marks)}</span>` : \"\"}${esc(label)}",
		`const tip = w.path + (claimHint(w) ? "\n" + claimHint(w) : "");`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("sidebar.js lacks %q", want)
		}
	}
}
