package promptstate

import (
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// FileStore keeps an atomic-rewrite TOML file at a caller-supplied path
// (one machine-global file, unlike searchhist's per-repo root).
type FileStore struct{ path string }

// NewFileStore points a store at the full file path (e.g. <state>/gg/prompts.toml).
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// records is the on-disk shape.
type records struct {
	SuppressedPrompts []string            `toml:"suppressed_prompts"`
	DismissedNotices  map[string][]string `toml:"dismissed_notices"`
	ApprovedTools     map[string][]string `toml:"approved_tools"`
	// A POINTER so "never saved" stays distinguishable from "saved, all
	// defaults" — see WebUIState.
	WebUI *WebUI `toml:"web_ui,omitempty"`
}

// read loads the file; a missing or malformed file reads as empty (UX memory
// is best-effort — never block the TUI on it).
func (fs *FileStore) read() records {
	empty := records{DismissedNotices: map[string][]string{}, ApprovedTools: map[string][]string{}}
	data, err := os.ReadFile(fs.path)
	if err != nil {
		return empty
	}
	var r records
	if err := toml.Unmarshal(data, &r); err != nil {
		return empty
	}
	if r.DismissedNotices == nil {
		r.DismissedNotices = map[string][]string{}
	}
	if r.ApprovedTools == nil {
		r.ApprovedTools = map[string][]string{}
	}
	return r
}

// SuppressedPrompts returns the globally suppressed prompt ids.
func (fs *FileStore) SuppressedPrompts() map[string]bool {
	return toSet(fs.read().SuppressedPrompts)
}

// SuppressPrompt records id as never-ask-again (idempotent) and persists.
func (fs *FileStore) SuppressPrompt(id string) error {
	r := fs.read() // read-merge: pick up any sibling writes first
	if toSet(r.SuppressedPrompts)[id] {
		return nil
	}
	r.SuppressedPrompts = append(r.SuppressedPrompts, id)
	return fs.write(r)
}

// DismissedNotices returns the notice ids dismissed for repoKey.
func (fs *FileStore) DismissedNotices(repoKey string) map[string]bool {
	return toSet(fs.read().DismissedNotices[repoKey])
}

// DismissNotice records a per-repo notice dismissal (idempotent) and persists.
func (fs *FileStore) DismissNotice(repoKey, id string) error {
	r := fs.read()
	if toSet(r.DismissedNotices[repoKey])[id] {
		return nil
	}
	r.DismissedNotices[repoKey] = append(r.DismissedNotices[repoKey], id)
	return fs.write(r)
}

// ApprovedToolCommands returns the tool-command hashes approved for repoKey.
func (fs *FileStore) ApprovedToolCommands(repoKey string) map[string]bool {
	return toSet(fs.read().ApprovedTools[repoKey])
}

// ApproveToolCommand records a per-repo tool-command approval (idempotent) and persists.
func (fs *FileStore) ApproveToolCommand(repoKey, hash string) error {
	r := fs.read()
	if toSet(r.ApprovedTools[repoKey])[hash] {
		return nil
	}
	r.ApprovedTools[repoKey] = append(r.ApprovedTools[repoKey], hash)
	return fs.write(r)
}

func toSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// write persists r via temp-file + rename (the searchhist / seq-state pattern).
func (fs *FileStore) write(r records) error {
	dir := filepath.Dir(fs.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "prompts-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, fs.path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
