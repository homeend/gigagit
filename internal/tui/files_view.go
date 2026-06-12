package tui

import (
	"path"
	"sort"

	"github.com/gigagit/gg/internal/model"
)

// commitFileLines renders a commit's changed files as content lines:
// root-level files first (no heading), then one bold heading per directory
// (its full path) with the directory's files indented beneath. Exactly one
// heading level — no nesting. Sorting is dir-major because a plain path sort
// interleaves a directory's files with its subdirectories, which would emit
// the same heading twice.
func commitFileLines(files []model.CommitFile) []contentLine {
	if len(files) == 0 {
		return []contentLine{{text: "(no files)"}}
	}
	sorted := make([]model.CommitFile, len(files))
	copy(sorted, files)
	sort.SliceStable(sorted, func(a, b int) bool {
		da, db := path.Dir(sorted[a].Path), path.Dir(sorted[b].Path)
		if da != db {
			return da < db // "." sorts before any directory name
		}
		return sorted[a].Path < sorted[b].Path
	})

	out := make([]contentLine, 0, len(sorted))
	lastDir := ""
	for _, f := range sorted {
		dir := path.Dir(f.Path)
		if dir == "." {
			out = append(out, contentLine{text: fileLine(f)})
			continue
		}
		if dir != lastDir {
			out = append(out, contentLine{text: dir + "/", heading: true})
			lastDir = dir
		}
		out = append(out, contentLine{text: "  " + fileLine(f)})
	}
	return out
}

// fileLine renders one file row: "<letter>  <basename>"; renames show the
// full old path and the new basename.
func fileLine(f model.CommitFile) string {
	if f.OldPath != "" {
		return f.Status + "  " + f.OldPath + " → " + path.Base(f.Path)
	}
	return f.Status + "  " + path.Base(f.Path)
}
