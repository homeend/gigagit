package domain

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// taskResultKeep bounds the result files written for viewing.
const taskResultKeep = 50

// SaveTaskResult writes an AI task's result (or output) as <id><ext> in
// its own state dir, so a frontend can open it in its file viewer like any
// other file. The directory keeps the newest taskResultKeep files.
func SaveTaskResult(id TaskID, ext, text string) (string, error) {
	dir := stateBaseDir("task-results")
	if dir == "" {
		return "", errors.New("no state dir available")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, string(id)+ext)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	pruneTaskResults(dir)
	return path, nil
}

// pruneTaskResults removes all but the newest taskResultKeep files.
func pruneTaskResults(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= taskResultKeep {
		return
	}
	type f struct {
		name string
		mod  int64
	}
	var fs []f
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() {
			fs = append(fs, f{e.Name(), info.ModTime().UnixNano()})
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].mod > fs[j].mod })
	for _, x := range fs[min(len(fs), taskResultKeep):] {
		_ = os.Remove(filepath.Join(dir, x.name))
	}
}
