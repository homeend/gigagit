package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// withXXLanguage installs a temp custom "xx" bundle whose [strings] table is
// extra, activates it, and registers a t.Cleanup restoring English — reusing
// language_test.go's setupCustomLang/langDirFromEnv mechanism (the
// footer_i18n_test.go pattern) rather than inventing a new one.
func withXXLanguage(t *testing.T, extra map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("[meta]\nname = \"Xxish\"\n\n[strings]\n")
	for k, v := range extra {
		fmt.Fprintf(&b, "%q = %q\n", k, v)
	}
	setupCustomLang(t, b.String())
	if err := i18n.SetLanguage("xx", langDirFromEnv(t)); err != nil {
		t.Fatal(err)
	}
}

func TestRenderSummaryFallbackWhenNoParts(t *testing.T) {
	res := engine.Result{Summary: "hand-built english"}
	if got := renderSummary(res); got != "hand-built english" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderSummaryTranslatesParts(t *testing.T) {
	withXXLanguage(t, map[string]string{
		"created branch %s": "XX-branch %s",
	})
	res := engine.Result{}.WithSummary("created branch %s", "feat/x")
	if got := renderSummary(res); got != "XX-branch feat/x" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderSummaryConcatsParts(t *testing.T) {
	withXXLanguage(t, map[string]string{
		"merged %s":                      "XX-merge %s",
		" (your changes remain stashed)": " XX-stashed",
	})
	res := engine.Result{}.WithSummary("merged %s", "a").
		AppendSummary(" (your changes remain stashed)")
	if got := renderSummary(res); got != "XX-merge a XX-stashed" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderProgressTranslatesStepAndKeepsDataDetail(t *testing.T) {
	withXXLanguage(t, map[string]string{"committing": "XX-commit"})
	e := engine.Progress{Step: "committing", Detail: "fix: raise to 100%"}
	if got := renderProgress(e); got != "XX-commit: fix: raise to 100%" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderProgressTranslatesDetailMsg(t *testing.T) {
	withXXLanguage(t, map[string]string{
		"rebasing":   "XX-rebase",
		"%s onto %s": "%[2]s XX-onto %[1]s",
	})
	e := engine.Progressf("rebasing", "%s onto %s", "feat/x", "main")
	if got := renderProgress(e); got != "XX-rebase: main XX-onto feat/x" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderPromptFallbackAndTranslated(t *testing.T) {
	withXXLanguage(t, map[string]string{"Delete branch %s?": "XX-del %s?"})
	if got := renderPrompt(engine.DecisionRequest{Prompt: "legacy english"}); got != "legacy english" {
		t.Fatalf("fallback got %q", got)
	}
	req := engine.PromptReq("branch.delete", "Delete branch %s?", []string{"delete", "cancel"}, "feat/x")
	if got := renderPrompt(req); got != "XX-del feat/x?" {
		t.Fatalf("got %q", got)
	}
}
