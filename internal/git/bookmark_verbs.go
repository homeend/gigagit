package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// CatFileBlob returns a blob's raw bytes by its object id (`git cat-file blob
// <sha>`). Used to resolve a bookmark to permanent (committed/shelf) content.
func (r *Repo) CatFileBlob(ctx context.Context, sha string) ([]byte, error) {
	argv := gitcmd.New("cat-file").Arg("blob", sha).ToArgv()
	res, err := r.Runner.Run(ctx, "git cat-file blob", argv)
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}

// ShowFileInDir runs `git -C <dir> show <rev>:<path>`, reading path at rev in the
// repo rooted at dir — used to read another worktree's index (rev == "" →
// `:path`). The -C global overrides cwd, so the Service's own workdir is
// irrelevant.
func (r *Repo) ShowFileInDir(ctx context.Context, dir, rev, path string) ([]byte, error) {
	argv := gitcmd.New("-C").Arg(dir, "show", rev+":"+path).ToArgv()
	res, err := r.Runner.Run(ctx, "git -C show", argv)
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}

// BlobSHA resolves the blob object id of path at rev (`git rev-parse
// <rev>:<path>`), captured when bookmarking a committed file.
func (r *Repo) BlobSHA(ctx context.Context, rev, path string) (string, error) {
	argv := gitcmd.New("rev-parse").Arg(rev + ":" + path).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse blob", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
