package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// UnmergedStages materializes an unmerged path's index stages through git's
// checkout conversion (smudge filters, CRLF) — reading the blobs with `git
// show` instead would return them in clean/LF form and a later resolution
// would silently rewrite the file's line endings. One checkout-index
// invocation writes every present stage to a temp file inside the worktree;
// the temps are read and removed before returning. base is nil when stage 1
// is absent (add/add); a missing stage 2 or 3 is an error, since a two-sided
// picker needs both.
func (r *Repo) UnmergedStages(ctx context.Context, path string) (base, current, incoming []byte, err error) {
	top, err := r.TopLevel(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	argv := gitcmd.New("checkout-index").Arg("--temp", "--stage=all", "--").Arg(path).ToArgv()
	res, err := r.Runner.Run(ctx, "git checkout-index", argv)
	if err != nil {
		return nil, nil, nil, err
	}
	// One line per input path: "<stage1> <stage2> <stage3>\t<path>", where an
	// absent stage is ".". A path that is not unmerged yields a single name —
	// reject that shape rather than misread it.
	line, _, _ := strings.Cut(strings.TrimRight(res.Stdout, "\n"), "\t")
	names := strings.Fields(line)
	cleanup := func() {
		for _, n := range names {
			if n != "." {
				os.Remove(filepath.Join(top, n))
			}
		}
	}
	defer cleanup()
	if len(names) != 3 {
		return nil, nil, nil, fmt.Errorf("git checkout-index: %q is not unmerged", path)
	}
	read := func(name string) ([]byte, error) {
		if name == "." {
			return nil, nil
		}
		return os.ReadFile(filepath.Join(top, name))
	}
	if base, err = read(names[0]); err != nil {
		return nil, nil, nil, err
	}
	if current, err = read(names[1]); err != nil {
		return nil, nil, nil, err
	}
	if incoming, err = read(names[2]); err != nil {
		return nil, nil, nil, err
	}
	if names[1] == "." || names[2] == "." {
		return nil, nil, nil, fmt.Errorf("git checkout-index: %q lacks a side (stages %s/%s)", path, names[1], names[2])
	}
	return base, current, incoming, nil
}

// RegenerateConflict re-merges the three stage contents with merge-file at an
// explicit marker size and returns the marker text. merge-file exits with the
// NUMBER OF CONFLICTS, so a positive exit is success — only a negative exit
// (internal error) fails. Inputs travel through temp files outside the
// worktree (merge-file takes paths and the Runner wires no stdin), and
// merge.conflictStyle is pinned to classic merge so a user's diff3
// configuration cannot reintroduce ||||||| sections the picker does not
// model.
func (r *Repo) RegenerateConflict(ctx context.Context, base, current, incoming []byte, markerSize int) ([]byte, error) {
	write := func(label string, content []byte) (string, error) {
		f, err := os.CreateTemp("", "gg-merge-"+label+"-*")
		if err != nil {
			return "", err
		}
		if _, err := f.Write(content); err != nil {
			f.Close()
			os.Remove(f.Name())
			return "", err
		}
		if err := f.Close(); err != nil {
			os.Remove(f.Name())
			return "", err
		}
		return f.Name(), nil
	}
	var tmps []string
	defer func() {
		for _, t := range tmps {
			os.Remove(t)
		}
	}()
	paths := make([]string, 0, 3)
	for _, in := range []struct {
		label   string
		content []byte
	}{{"current", current}, {"base", base}, {"incoming", incoming}} {
		p, err := write(in.label, in.content)
		if err != nil {
			return nil, err
		}
		tmps = append(tmps, p)
		paths = append(paths, p)
	}
	argv := gitcmd.New("-c").Arg("merge.conflictStyle=merge").
		Arg("merge-file", "-p", fmt.Sprintf("--marker-size=%d", markerSize)).
		Arg("-L", "current", "-L", "base", "-L", "incoming").
		Arg(paths[0], paths[1], paths[2]).ToArgv()
	res, err := r.Runner.Run(ctx, "git merge-file", argv)
	if err != nil && res.ExitCode <= 0 {
		return nil, err
	}
	return []byte(res.Stdout), nil
}
