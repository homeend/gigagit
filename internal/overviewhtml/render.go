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

// MaxBytes caps the rendered page. Not a browser limit (browsers open far
// larger pages); a guard against an accidental giant file to share.
const MaxBytes = 30 << 20

// TooLargeError: the page would pass MaxBytes. Largest names the biggest
// files ("path (x MB)", largest first, at most three) — what to drop.
// AtLeast: Size is a lower bound (Oversized stopped before the page was
// built), else it is the page's own size.
type TooLargeError struct {
	Size    int
	AtLeast bool
	Largest []string
}

func (e *TooLargeError) Error() string {
	if e.AtLeast {
		return fmt.Sprintf("overview export is at least %d MB, over the %d MB cap; largest files: %s",
			e.Size>>20, MaxBytes>>20, strings.Join(e.Largest, ", "))
	}
	return fmt.Sprintf("overview export is %d MB, over the %d MB cap; largest files: %s",
		mb(e.Size), MaxBytes>>20, strings.Join(e.Largest, ", "))
}

// Oversized refuses files whose text alone passes MaxBytes — the builder
// asks after each file it reads, so a giant export stops early. The page
// only grows from there (JSON escaping, token runs, the assets), so the
// size it names is a lower bound.
func Oversized(files []File) error {
	n := 0
	for _, f := range files {
		n += fileSize(f)
	}
	if n <= MaxBytes {
		return nil
	}
	return tooLarge(files, n, true)
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
// and U+2028/9, so no text in the blob can close its tag. Over MaxBytes it
// refuses with the page's own size (the builder's Oversized has already
// stopped a giant export before it read everything).
func Render(e Export) ([]byte, error) {
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
	if size := len(page) + len(styles) + len(data) + len(script.String()) + len(e.Title); size > MaxBytes {
		return nil, tooLarge(e.Files, size, false)
	}
	out := strings.NewReplacer(
		"{{TITLE}}", html.EscapeString(e.Title),
		"{{CSS}}", styles,
		"{{DATA}}", string(data),
		"{{SCRIPT}}", script.String(),
	).Replace(string(page))
	return []byte(out), nil
}

// Each line's and diff row's own JSON beside its text (keys, quotes,
// numbers at their shortest): what an all-short-lines file weighs.
var (
	lineOverhead = jsonLen(Line{}) + 1    // {"text":""},
	rowOverhead  = jsonLen(DiffRow{}) + 1 // {"kind":"","left":"",…},
)

func jsonLen(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}

// fileSize is a lower bound of f's weight in the blob: its text, each
// line's and row's JSON, 16 bytes a token run (escaping only adds).
func fileSize(f File) int {
	n := 0
	for _, l := range f.Lines {
		n += lineOverhead + len(l.Text) + 16*len(l.Tok)
	}
	for _, r := range f.Diff {
		n += rowOverhead + len(r.Left) + len(r.Right) + 16*(len(r.LeftTok)+len(r.RightTok)+len(r.LeftSpans)+len(r.RightSpans))
	}
	return n
}

func tooLarge(files []File, size int, atLeast bool) error {
	fs := append([]File(nil), files...)
	sort.SliceStable(fs, func(i, j int) bool { return fileSize(fs[i]) > fileSize(fs[j]) })
	var big []string
	for _, f := range fs[:min(3, len(fs))] {
		big = append(big, fmt.Sprintf("%s (%d MB)", f.Path, mb(fileSize(f))))
	}
	return &TooLargeError{Size: size, AtLeast: atLeast, Largest: big}
}
