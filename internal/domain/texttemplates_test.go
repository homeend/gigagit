package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/texttmpl"
)

func textSvc(t *testing.T) *Service {
	t.Helper()
	_, svc := newRealRepo(t)
	svc.SetTextTemplateStores(
		texttmpl.NewFileStore(t.TempDir(), model.ProfileScopeGlobal),
		texttmpl.NewFileStore(t.TempDir(), model.ProfileScopeRepo))
	return svc
}

func TestTextTemplatesAddListScopes(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	if _, err := svc.AddTextTemplate(ctx, model.TextTemplate{Title: "G", Body: "g", Scope: model.ProfileScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTextTemplate(ctx, model.TextTemplate{Title: " R ", Body: "r\n\n", Scope: model.ProfileScopeRepo}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.TextTemplates(ctx)
	if err != nil || len(list) != 2 || list[0].Title != "G" || list[1].Scope != model.ProfileScopeRepo {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[1].Title != "R" || list[1].Body != "r" {
		t.Fatalf("title/body not trimmed: %+v", list[1])
	}
}

func TestTextTemplateUpdateRemoveAndErrorKinds(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	g := model.ProfileScopeGlobal
	a, _ := svc.AddTextTemplate(ctx, model.TextTemplate{Title: "A", Body: "a", Scope: g})
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "B", Body: "b", Scope: g})

	up, err := svc.UpdateTextTemplate(ctx, g, a.ID, model.TextTemplate{Title: "A2", Body: "new"})
	if err != nil || up.ID != "a2" || up.Body != "new" {
		t.Fatalf("update = %+v, %v", up, err)
	}
	if _, err := svc.UpdateTextTemplate(ctx, g, "a2", model.TextTemplate{Title: "B", Body: "x"}); !IsTextTemplateDuplicate(err) {
		t.Fatalf("rename onto B: %v", err)
	}
	if _, err := svc.AddTextTemplate(ctx, model.TextTemplate{Title: "b", Body: "x", Scope: g}); !IsTextTemplateDuplicate(err) {
		t.Fatalf("duplicate add: %v", err)
	}
	if err := svc.RemoveTextTemplate(ctx, g, "nope"); !IsTextTemplateNotFound(err) {
		t.Fatalf("remove unknown: %v", err)
	}
	if err := svc.RemoveTextTemplate(ctx, g, "a2"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTextTemplate(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", MaxTextTemplateBody+1)
	for name, tc := range map[string]struct{ title, body string }{
		"empty title":     {"  ", "b"},
		"two-line title":  {"a\nb", "b"},
		"long title":      {strings.Repeat("t", 81), "b"},
		"symbol title":    {"!!!", "b"},
		"empty body":      {"t", " \n\t"},
		"oversized body":  {"t", long},
		"malformed token": {"t", "x <seq> y"},
	} {
		if err := ValidateTextTemplate(tc.title, tc.body); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if err := ValidateTextTemplate("t", "ok <br> <user:a> <branch>"); err != nil {
		t.Fatalf("valid template refused: %v", err)
	}
}

func TestTextTemplateTokens(t *testing.T) {
	t.Parallel()
	labels, auto := TextTemplateTokens("<user:a> <date> <br> <user:b>")
	if len(labels) != 2 || labels[1] != "b" || len(auto) != 1 || auto[0] != "<date>" {
		t.Fatalf("labels %v auto %v", labels, auto)
	}
}

func TestRenderPeeksTakeConsumes(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	body := "n=<seq:tt:2> <user:who> on <branch>"
	in := map[string]string{"who": "me"}
	for i := 0; i < 2; i++ { // two previews: the same number
		got, seqs, err := svc.RenderTextTemplate(ctx, body, in)
		if err != nil || got != "n=01 me on main" || len(seqs) != 1 || seqs[0] != "tt" {
			t.Fatalf("render %d = %q %v %v", i, got, seqs, err)
		}
	}
	if got, err := svc.TakeTextTemplate(ctx, body, in); err != nil || got != "n=01 me on main" {
		t.Fatalf("take = %q, %v", got, err)
	}
	if got, _, _ := svc.RenderTextTemplate(ctx, body, in); got != "n=02 me on main" {
		t.Fatalf("after take the preview = %q, want n=02 …", got)
	}
	// A take that cannot resolve consumes nothing.
	if _, err := svc.TakeTextTemplate(ctx, body, nil); err == nil {
		t.Fatal("take without the input must fail")
	}
	if got, _, _ := svc.RenderTextTemplate(ctx, body, in); got != "n=02 me on main" {
		t.Fatalf("a failed take consumed a number: %q", got)
	}
}

func TestFindTextTemplate(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Note", Body: "global", Scope: model.ProfileScopeGlobal})
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Note", Body: "repo", Scope: model.ProfileScopeRepo})
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Notice", Body: "n", Scope: model.ProfileScopeGlobal})

	if got, err := svc.FindTextTemplate(ctx, "note", nil); err != nil || got.Body != "repo" {
		t.Fatalf("exact id, repo wins: %+v, %v", got, err)
	}
	g := model.ProfileScopeGlobal
	if got, err := svc.FindTextTemplate(ctx, "note", &g); err != nil || got.Body != "global" {
		t.Fatalf("explicit scope: %+v, %v", got, err)
	}
	if got, err := svc.FindTextTemplate(ctx, "notic", nil); err != nil || got.Title != "Notice" {
		t.Fatalf("unique prefix: %+v, %v", got, err)
	}
	if _, err := svc.FindTextTemplate(ctx, "no", nil); err == nil || IsTextTemplateNotFound(err) {
		t.Fatalf("ambiguous prefix must be an ambiguity error: %v", err)
	}
	if _, err := svc.FindTextTemplate(ctx, "zzz", nil); !IsTextTemplateNotFound(err) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestValidateTextTemplateRefusesInertTokensAndTabTitle(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ title, body string }{
		"branch with an argument": {"t", "on <branch:short>"},
		"user without a label":    {"t", "hi <user:>"},
		"tab in the title":        {"a\tb", "b"},
		"control char in title":   {"a\x1bb", "b"},
	} {
		if err := ValidateTextTemplate(tc.title, tc.body); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// A prefix that names ONE id held by both scopes is not ambiguous: the repo
// row wins, as it does for the exact id.
func TestFindTextTemplatePrefixInBothScopesRepoWins(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Zebra", Body: "global", Scope: model.ProfileScopeGlobal})
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Zebra", Body: "repo", Scope: model.ProfileScopeRepo})
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Yak one", Body: "a", Scope: model.ProfileScopeGlobal})
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Yak two", Body: "b", Scope: model.ProfileScopeRepo})

	if got, err := svc.FindTextTemplate(ctx, "ze", nil); err != nil || got.Body != "repo" {
		t.Fatalf("prefix of one id in both scopes: %+v, %v", got, err)
	}
	_, err := svc.FindTextTemplate(ctx, "yak", nil)
	if err == nil || IsTextTemplateNotFound(err) || !strings.Contains(err.Error(), "matches 2") {
		t.Fatalf("two distinct ids must stay ambiguous: %v", err)
	}
	// The domain names no frontend's command.
	if _, err := svc.FindTextTemplate(ctx, "nope", nil); !IsTextTemplateNotFound(err) || strings.Contains(err.Error(), "gg template") {
		t.Fatalf("unknown id: %v", err)
	}
}

// Several counters are taken together and the text carries the numbers taken.
func TestTakeTextTemplateSeveralCounters(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	body := "<seq:one>/<seq:two>/<seq:one>"
	for _, want := range []string{"1/1/1", "2/2/2"} {
		if got, err := svc.TakeTextTemplate(ctx, body, nil); err != nil || got != want {
			t.Fatalf("take = %q, %v; want %q", got, err, want)
		}
	}
}

// One damaged scope file does not take the other scope down: its rows are
// still listed and found, and the damage is still reported.
func TestTextTemplatesDamagedScopeKeepsTheOther(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	repoDir := t.TempDir()
	svc.SetTextTemplateStores(
		texttmpl.NewFileStore(t.TempDir(), model.ProfileScopeGlobal),
		texttmpl.NewFileStore(repoDir, model.ProfileScopeRepo))
	ctx := context.Background()
	if _, err := svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Healthy", Body: "g", Scope: model.ProfileScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "texttemplates.toml"), []byte("[[templates]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := svc.TextTemplates(ctx)
	if err == nil || len(list) != 1 || list[0].Title != "Healthy" {
		t.Fatalf("list = %+v, err %v; want the healthy row AND the error", list, err)
	}
	if got, err := svc.FindTextTemplate(ctx, "healthy", nil); err != nil || got.Body != "g" {
		t.Fatalf("find in the healthy scope: %+v, %v", got, err)
	}
	// What is not found may be in the damaged file: that is the error to give.
	if _, err := svc.FindTextTemplate(ctx, "other", nil); err == nil || IsTextTemplateNotFound(err) {
		t.Fatalf("find a missing id with a damaged scope: %v", err)
	}
}
