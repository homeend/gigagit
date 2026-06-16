package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// StageBlob sets the index entry for path to exactly content, without touching
// the working tree: hash-object writes a blob (with --path so clean filters
// apply as if the bytes were at path), then update-index --cacheinfo rewrites
// the index entry. The mode is taken from the file's current index entry, so
// the executable bit is preserved. The Runner has no stdin, so the content is
// hashed from a temp file outside the working tree.
func (r *Repo) StageBlob(ctx context.Context, path string, content []byte) error {
	mode, err := r.indexMode(ctx, path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "gg-stage-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	res, err := r.Runner.Run(ctx, "git hash-object",
		gitcmd.New("hash-object").Arg("-w", "--path="+path, "--").Arg(tmp.Name()).ToArgv())
	if err != nil {
		return err
	}
	sha := strings.TrimSpace(res.Stdout)
	if sha == "" {
		return fmt.Errorf("hash-object: empty sha for %s", path)
	}
	_, err = r.Runner.Run(ctx, "git update-index",
		gitcmd.New("update-index").Arg("--cacheinfo", mode+","+sha+","+path).ToArgv())
	return err
}

// indexMode returns the octal mode (e.g. "100644") of path's current index
// entry. An empty result means the path is not in the index.
func (r *Repo) indexMode(ctx context.Context, path string) (string, error) {
	res, err := r.Runner.Run(ctx, "git ls-files",
		gitcmd.New("ls-files").Arg("-s", "--").Arg(path).ToArgv())
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return "", fmt.Errorf("stage: %s is not tracked", path)
	}
	// format: "<mode> <sha> <stage>\t<path>"
	mode, _, ok := strings.Cut(out, " ")
	if !ok {
		return "", fmt.Errorf("stage: cannot parse ls-files output %q", out)
	}
	return mode, nil
}
