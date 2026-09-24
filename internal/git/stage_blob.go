package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// Blob is one index entry to set: path's staged content becomes Content.
type Blob struct {
	Path    string
	Content []byte
}

// StageBlobs sets the index entries for several paths to exactly the given
// contents, without touching the working tree: hash-object writes each blob
// (with --path so clean filters apply as if the bytes were at that path),
// then ONE update-index --cacheinfo rewrites every entry. The modes are
// taken from the files' current index entries (one ls-files), so the
// executable bit is preserved. The Runner has no stdin, so each content is
// hashed from a temp file outside the working tree. Calls are batched
// because each git process is the cost on a slow filesystem.
func (r *Repo) StageBlobs(ctx context.Context, blobs []Blob) error {
	if len(blobs) == 0 {
		return nil
	}
	paths := make([]string, len(blobs))
	for i, b := range blobs {
		paths[i] = b.Path
	}
	modes, err := r.indexModes(ctx, paths)
	if err != nil {
		return err
	}
	update := gitcmd.New("update-index")
	for _, b := range blobs {
		sha, err := r.hashBlob(ctx, b)
		if err != nil {
			return err
		}
		update.Arg("--cacheinfo", modes[b.Path]+","+sha+","+b.Path)
	}
	_, err = r.Runner.Run(ctx, "git update-index", update.ToArgv())
	return err
}

// hashBlob writes one blob into the object store and returns its sha.
func (r *Repo) hashBlob(ctx context.Context, b Blob) (string, error) {
	tmp, err := os.CreateTemp("", "gg-stage-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b.Content); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	res, err := r.Runner.Run(ctx, "git hash-object",
		gitcmd.New("hash-object").Arg("-w", "--path="+b.Path, "--").Arg(tmp.Name()).ToArgv())
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(res.Stdout)
	if sha == "" {
		return "", fmt.Errorf("hash-object: empty sha for %s", b.Path)
	}
	return sha, nil
}

// indexModes returns the octal mode (e.g. "100644") of each path's current
// index entry, in one ls-files. A path with no entry is an error.
func (r *Repo) indexModes(ctx context.Context, paths []string) (map[string]string, error) {
	res, err := r.Runner.Run(ctx, "git ls-files",
		gitcmd.New("ls-files").Arg("-s", "-z", "--").Arg(paths...).ToArgv())
	if err != nil {
		return nil, err
	}
	modes := make(map[string]string, len(paths))
	for _, rec := range strings.Split(res.Stdout, "\x00") {
		if rec == "" {
			continue
		}
		// format: "<mode> <sha> <stage>\t<path>"
		meta, path, ok := strings.Cut(rec, "\t")
		mode, _, ok2 := strings.Cut(meta, " ")
		if !ok || !ok2 {
			return nil, fmt.Errorf("stage: cannot parse ls-files output %q", rec)
		}
		modes[path] = mode
	}
	for _, p := range paths {
		if modes[p] == "" {
			return nil, fmt.Errorf("stage: %s is not tracked", p)
		}
	}
	return modes, nil
}
