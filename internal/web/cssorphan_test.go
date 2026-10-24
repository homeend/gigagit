package web

import (
	"regexp"
	"strings"
	"testing"
)

// A rule whose selector line was deleted leaves its declarations at the top
// level, and a CSS parser then reads them — and the stray "}" — into the NEXT
// rule's selector, silently dropping that rule (it happened: removing the
// park chip's selector took the disabled pull/push/refresh dimming with it).
// Every stylesheet's top level holds selectors and at-rules only.
func TestStylesheetsHaveNoOrphanDeclarations(t *testing.T) {
	t.Parallel()
	comments := regexp.MustCompile(`(?s)/\*.*?\*/`)
	for _, f := range []string{"style.css", "polish.css"} {
		src := comments.ReplaceAllString(readStatic(t, f), "")
		depth, top := 0, strings.Builder{}
		for _, c := range src {
			switch c {
			case '{':
				if depth == 0 {
					if pre := top.String(); strings.Contains(pre, ";") && !strings.Contains(pre, "@") {
						t.Errorf("%s: declarations outside any rule before %q", f, strings.TrimSpace(pre[strings.LastIndex(pre, ";")+1:]))
					}
					top.Reset()
				}
				depth++
			case '}':
				depth--
				if depth < 0 {
					t.Fatalf("%s: a stray }", f)
				}
			default:
				if depth == 0 {
					top.WriteRune(c)
				}
			}
		}
	}
}
