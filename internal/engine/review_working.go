package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// workingInput is what a working-changes review adds to `git diff HEAD`
// (spec 2026-10-04 working reviews §5–6): a fingerprint of every file the
// review reads, and the untracked files as new-file patches plus their
// numstat records (NUL-terminated, as `--numstat -z` prints them).
type workingInput struct {
	files   []model.NoteFile
	patch   string
	numstat string
	overCap bool // a file was left out of the patch because it alone breaks the cap
}

// reviewWorkingInput fingerprints the paths of the working diff (stats) and
// every untracked file, and synthesizes the untracked files' patches while
// the diff (diffBytes so far) stays under MaxDiffBytes. Paths are read from
// the worktree's top level, never op.Dir (which may be a subdirectory).
func reviewWorkingInput(ctx context.Context, deps OpDeps, stats []model.DiffStat, diffBytes int) (workingInput, error) {
	top, err := deps.Repo.TopLevel(ctx)
	if err != nil {
		return workingInput{}, err
	}
	top = strings.TrimSpace(top)
	format, ferr := deps.Repo.ObjectFormat(ctx)
	if ferr != nil || format == "" {
		format = "sha1"
	}
	var w workingInput
	seen := map[string]bool{}
	abs := func(p string) string { return filepath.Join(top, filepath.FromSlash(p)) }
	add := func(p string, b git.WorktreeBlob, err error) bool {
		f := model.NoteFile{Path: p}
		switch {
		case errors.Is(err, fs.ErrNotExist):
			f.Deleted = true
		case err != nil:
			return false // present but unreadable: it proves nothing, so it is not fingerprinted
		default:
			f.Blob = b.ID
		}
		w.files = append(w.files, f)
		return true
	}
	for _, st := range stats {
		for _, p := range []string{st.OldPath, st.Path} {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			b, herr := git.HashWorktreeFile(format, abs(p))
			add(p, b, herr)
		}
	}
	untracked, err := deps.Repo.UntrackedFiles(ctx)
	if err != nil {
		return workingInput{}, err
	}
	var patch, numstat strings.Builder
	size := diffBytes
	for _, p := range untracked {
		if seen[p] || strings.HasSuffix(p, "/") { // "sub/" = a nested repository, not a file
			continue
		}
		seen[p] = true
		full := abs(p)
		if fi, serr := os.Lstat(full); serr == nil && fi.Mode().IsRegular() && size <= MaxDiffBytes &&
			fi.Size() <= int64(MaxDiffBytes-size) {
			if data, rerr := os.ReadFile(full); rerr == nil {
				b := git.BlobOf(format, data)
				add(p, b, nil)
				text := newFilePatch(p, fi.Mode(), data, b.Binary)
				patch.WriteString(text)
				size += len(text)
				numstat.WriteString(numstatRecord(p, b))
				continue
			}
		}
		b, herr := git.HashWorktreeFile(format, full)
		if add(p, b, herr) && !errors.Is(herr, fs.ErrNotExist) {
			numstat.WriteString(numstatRecord(p, b))
			w.overCap = true
		}
	}
	w.patch, w.numstat = patch.String(), numstat.String()
	return w, nil
}

// numstatRecord is one `--numstat -z` record for an added file.
func numstatRecord(path string, b git.WorktreeBlob) string {
	if b.Binary {
		return "-\t-\t" + path + "\x00"
	}
	return fmt.Sprintf("%d\t0\t%s\x00", b.Lines, path)
}

// newFilePatch is git's patch for an untracked file as if it were added:
// every line added, a binary file only named, an empty one header-only.
func newFilePatch(path string, mode fs.FileMode, data []byte, binary bool) string {
	var b strings.Builder
	fileMode := "100644"
	if mode&0o111 != 0 {
		fileMode = "100755"
	}
	fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode %s\n", path, path, fileMode)
	switch {
	case len(data) == 0:
		return b.String()
	case binary:
		fmt.Fprintf(&b, "Binary files /dev/null and b/%s differ\n", path)
		return b.String()
	}
	text := string(data)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	count := fmt.Sprintf("1,%d", len(lines))
	if len(lines) == 1 {
		count = "1" // git drops ",1"
	}
	fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n@@ -0,0 +%s @@\n", path, count)
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\\ No newline at end of file\n")
	}
	return b.String()
}
