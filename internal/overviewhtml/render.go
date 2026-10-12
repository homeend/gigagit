package overviewhtml

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/web/static"
)

//go:embed assets
var assets embed.FS

// MaxBytes caps the rendered page.
const MaxBytes = 16 << 20

// TooLargeError: the page would pass MaxBytes. Largest names the biggest
// files ("path (x MB)", largest first, at most three) — what to drop.
type TooLargeError struct {
	Size    int
	Largest []string
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("overview export is %d MB, over the %d MB cap; largest files: %s",
		mb(e.Size), MaxBytes>>20, strings.Join(e.Largest, ", "))
}

func mb(n int) int { return (n + 1<<20 - 1) >> 20 }

// modules are the gg web painters the page reuses and the names its script
// calls on each (MD.mdHTML, SD.unifiedRows).
var modules = []struct {
	name, file string
	exports    []string
}{
	{"MD", "markdown.js", []string{"mdHTML"}},
	{"SD", "splitdiff.js", []string{"unifiedRows"}},
}

var (
	exportList = regexp.MustCompile(`(?m)^export \{[^}]*\};?[ \t]*$`)
	exportKw   = regexp.MustCompile(`(?m)^export (function|const|let|class) `)
	moduleSyn  = regexp.MustCompile(`(?m)^\s*(import|export)\b`)
)

// scopeModule turns an import-free ES module into a block-scoped classic
// script whose exports are the fields of const name: a page opened from
// disk loads no module, so nothing depends on how a browser treats file://
// module imports. Any import, or an export form it does not know, is an
// error rather than a broken page.
func scopeModule(name, src string, exports []string) (string, error) {
	body := exportList.ReplaceAllString(src, "")
	body = exportKw.ReplaceAllString(body, "$1 ")
	if loc := moduleSyn.FindStringIndex(body); loc != nil {
		line := body[loc[0]:]
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		return "", fmt.Errorf("%s: module syntax left: %q", name, strings.TrimSpace(line))
	}
	return "const " + name + " = (() => {\n" + body + "\nreturn { " + strings.Join(exports, ", ") + " };\n})();\n", nil
}

// Render is e as one self-contained HTML page: gg web's style.css and the
// page's own layout in one <style>, e as a JSON blob, and one classic
// <script> (the scoped painters + export.js). json.Marshal escapes < > &
// and U+2028/9, so no text in the blob can close its tag.
func Render(e Export) ([]byte, error) {
	if size := approxSize(e); size > MaxBytes {
		return nil, tooLarge(e, size)
	}
	css, err := static.FS.ReadFile("style.css")
	if err != nil {
		return nil, err
	}
	own, err := assets.ReadFile("assets/export.css")
	if err != nil {
		return nil, err
	}
	var script strings.Builder
	for _, m := range modules {
		src, err := static.FS.ReadFile(m.file)
		if err != nil {
			return nil, err
		}
		s, err := scopeModule(m.name, string(src), m.exports)
		if err != nil {
			return nil, err
		}
		script.WriteString(s)
	}
	js, err := assets.ReadFile("assets/export.js")
	if err != nil {
		return nil, err
	}
	script.Write(js)
	styles := string(css) + "\n" + string(own)
	for _, part := range []string{styles, script.String()} {
		if l := strings.ToLower(part); strings.Contains(l, "</script") || strings.Contains(l, "</style") {
			return nil, errors.New("overview export: an inlined asset closes its own tag")
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	page, err := assets.ReadFile("assets/page.html")
	if err != nil {
		return nil, err
	}
	out := strings.NewReplacer(
		"{{TITLE}}", html.EscapeString(e.Title),
		"{{CSS}}", styles,
		"{{DATA}}", string(data),
		"{{SCRIPT}}", script.String(),
	).Replace(string(page))
	if len(out) > MaxBytes {
		return nil, tooLarge(e, len(out))
	}
	return []byte(out), nil
}

// fileSize approximates f's weight in the blob.
func fileSize(f File) int {
	n := 0
	for _, l := range f.Lines {
		n += len(l.Text) + 16*len(l.Tok)
	}
	for _, r := range f.Diff {
		n += len(r.Left) + len(r.Right) + 16*(len(r.LeftTok)+len(r.RightTok))
	}
	return n
}

func approxSize(e Export) int {
	n := 0
	for _, f := range e.Files {
		n += fileSize(f)
	}
	return n
}

func tooLarge(e Export, size int) error {
	fs := append([]File(nil), e.Files...)
	sort.SliceStable(fs, func(i, j int) bool { return fileSize(fs[i]) > fileSize(fs[j]) })
	var big []string
	for _, f := range fs[:min(3, len(fs))] {
		big = append(big, fmt.Sprintf("%s (%d MB)", f.Path, mb(fileSize(f))))
	}
	return &TooLargeError{Size: size, Largest: big}
}
