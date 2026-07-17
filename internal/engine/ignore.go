package engine

import (
	"context"
	"path"
	"strings"
)

// Ignore appends a single pattern to the repo-root .gitignore as an unstaged
// change. Path is the repo-relative path of the selected file. Ext true →
// "*<ext>" (the whole extension); else the file anchored at the repo root with
// glob metacharacters escaped. A pattern already present is a no-op. Default
// (TreeWrite) reservation — it touches the working tree.
type Ignore struct {
	Path string
	Ext  bool
}

var _ Operation = Ignore{}

func (op Ignore) Run(ctx context.Context, deps OpDeps) (Result, error) {
	line := ignoreLine(op.Path, op.Ext)
	existing, _ := deps.Repo.ReadWorktreeFile(ctx, ".gitignore") // absent → nil
	if alreadyIgnored(existing, line) {
		res := Result{}.WithSummary("%s already in .gitignore", line)
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	if err := deps.Repo.WriteWorktreeFile(ctx, ".gitignore", appendIgnoreLine(existing, line)); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("ignored %s", line)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// escapeIgnorePattern backslash-escapes the gitignore glob metacharacters so a
// literal filename matches itself. Backslash is escaped first so it does not
// double up the escapes inserted for the later metacharacters.
func escapeIgnorePattern(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`)
	return r.Replace(p)
}

// ignoreLine builds the .gitignore pattern for a file. ext true → "*<ext>"
// (inherently a glob, unescaped); else the file anchored at the repo root with
// metacharacters escaped.
func ignoreLine(p string, ext bool) string {
	if ext {
		return "*" + path.Ext(p)
	}
	return "/" + escapeIgnorePattern(p)
}

// alreadyIgnored reports whether line is already a pattern in content, after
// trimming, skipping blank and #-comment lines.
func alreadyIgnored(content []byte, line string) bool {
	for _, l := range strings.Split(string(content), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if t == line {
			return true
		}
	}
	return false
}

// appendIgnoreLine appends line + "\n" to content, inserting a separating
// newline when content is non-empty and unterminated.
func appendIgnoreLine(content []byte, line string) []byte {
	out := content
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(out, []byte(line+"\n")...)
}
