package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every surface that shows a file colours it through domain's Highlighter —
// one content-keyed store, shared while a lex runs and kept after — so the
// frontends and domain's own views never call the lexer themselves (the
// overview export and markdown code blocks are not file views).
func TestShownFilesLexThroughHighlighter(t *testing.T) {
	t.Parallel()
	direct := regexp.MustCompile(`\bsyntax\.Lex(Ctx)?\(`)
	allowed := map[string]bool{"domain/difftok.go": true, "domain/overview_export.go": true}
	for _, dir := range []string{"../tui", "../web", "../domain", "../cli", "../mcp"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			rel := strings.TrimPrefix(filepath.ToSlash(f), "../")
			if allowed[rel] {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for n, line := range strings.Split(string(src), "\n") {
				if direct.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
					t.Errorf("%s:%d lexes directly — go through domain's Highlighter: %s", rel, n+1, strings.TrimSpace(line))
				}
			}
		}
	}
}
