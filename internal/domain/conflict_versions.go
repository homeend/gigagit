package domain

import (
	"context"
	"os"
	"path/filepath"
)

// conflictVersions is the query result shape (query() returns one value).
type conflictVersions struct{ local, base, remote string }

// ConflictFileVersions materializes the index stages of a conflicted path
// into temp files for an external per-file merge tool: local = stage :2
// (ours), remote = stage :3 (theirs), base = stage :1 (common ancestor) —
// or an empty temp file when hasBase is false (an add/add conflict has no
// stage 1; git mergetool behaves the same). Temp names keep the real file
// extension so GUI tools syntax-highlight. cleanup removes all three files;
// callers must invoke it after the tool exits (best-effort).
func (s *Service) ConflictFileVersions(ctx context.Context, path string, hasBase bool) (local, base, remote string, cleanup func(), err error) {
	v, err := query(ctx, s, "conflictversions:"+path, func(ctx context.Context) (conflictVersions, error) {
		var out conflictVersions
		var made []string
		fail := func(err error) (conflictVersions, error) {
			for _, p := range made {
				os.Remove(p)
			}
			return conflictVersions{}, err
		}
		write := func(kind, rev string) (string, error) {
			var data []byte
			if rev != "" {
				var err error
				if data, err = s.repo.ShowFile(ctx, rev, path); err != nil {
					return "", err
				}
			}
			f, err := os.CreateTemp("", "gg-"+kind+"-*"+filepath.Ext(path))
			if err != nil {
				return "", err
			}
			made = append(made, f.Name())
			if _, err := f.Write(data); err != nil {
				f.Close()
				return "", err
			}
			return f.Name(), f.Close()
		}
		var werr error
		if out.local, werr = write("local", ":2"); werr != nil {
			return fail(werr)
		}
		baseRev := ""
		if hasBase {
			baseRev = ":1"
		}
		if out.base, werr = write("base", baseRev); werr != nil {
			return fail(werr)
		}
		if out.remote, werr = write("remote", ":3"); werr != nil {
			return fail(werr)
		}
		return out, nil
	})
	if err != nil {
		return "", "", "", nil, err
	}
	cleanup = func() {
		os.Remove(v.local)
		os.Remove(v.base)
		os.Remove(v.remote)
	}
	return v.local, v.base, v.remote, cleanup, nil
}
