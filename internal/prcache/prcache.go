// Package prcache is the on-disk pull-request cache: per repository, the PR
// list and one entry per recently opened PR (its forge data, its comments and
// the derived git data of its base/head pair). Records only — domain decides
// what is fresh and what to draw. JSON files under one directory, written
// temp + rename under the shared file lock, so several gg processes on one
// repository never tear a file. DAG leaf: stdlib, filelock, model.
package prcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
)

// DefaultMax bounds the PR entries kept per repository (spec §2.1).
const DefaultMax = 50

// futureSlack is how far ahead of now a read time may be and still count:
// two processes' clocks and a file system's mtime granularity disagree by
// seconds, never by hours.
const futureSlack = time.Minute

// Derived is the git data of one (merge base, head) pair: the diff a PR
// shows is merge-base..head, so the moving tip of its base branch is not
// part of the key — a base branch that moved without a new merge base is
// still a hit. A new push is a new head, a merge of the base into the PR a
// new merge base: either way a new key.
type Derived struct {
	MergeBase  string             `json:"merge_base"`
	Source     string             `json:"source"`
	Files      int                `json:"files,omitempty"`
	Commits    []string           `json:"commits,omitempty"`
	FileList   []model.CommitFile `json:"file_list,omitempty"`
	ComputedAt time.Time          `json:"computed_at"`
}

// Repo is the forge's base repository (asked once, then cached).
type Repo struct {
	Slug   string    `json:"slug"`
	URL    string    `json:"url"`
	ReadAt time.Time `json:"read_at"`
}

// Entry is one cached pull request.
type Entry struct {
	Number      int                  `json:"number"`
	PR          model.PullRequest    `json:"pr"`
	Full        bool                 `json:"full,omitempty"`
	Comments    []model.ForgeComment `json:"comments,omitempty"`
	Truncated   bool                 `json:"truncated,omitempty"`
	HasComments bool                 `json:"has_comments,omitempty"`
	ReadAt      time.Time            `json:"read_at"`
	OpenedAt    time.Time            `json:"opened_at"`
	Derived     []Derived            `json:"derived,omitempty"`
	// Groups maps a forge review gg sent to the local group it came from
	// ("mine", "review:<id>"): the review keeps its group's colour.
	Groups map[string]string `json:"groups,omitempty"`
}

// List is the cached open-PR listing.
type List struct {
	ReadAt   time.Time           `json:"read_at"`
	Provider string              `json:"provider"`
	PRs      []model.PullRequest `json:"prs"`
}

// Store is one repository's cache directory.
type Store struct {
	root string
	max  int
}

// New opens the cache rooted at root (created on first write), keeping at
// most max PR entries (<= 0 = DefaultMax).
func New(root string, max int) *Store {
	if max <= 0 {
		max = DefaultMax
	}
	return &Store{root: root, max: max}
}

// Fresh reports whether a read at readAt is still within maxAge of now. A
// zero time, a zero limit, or a read time more than a minute in the future
// (a clock moved back) is never fresh.
func Fresh(readAt, now time.Time, maxAge time.Duration) bool {
	if readAt.IsZero() || maxAge <= 0 || readAt.After(now.Add(futureSlack)) {
		return false
	}
	return now.Sub(readAt) < maxAge
}

// PutDerived records d, replacing the same pair and keeping the newest two.
func (e *Entry) PutDerived(d Derived) {
	out := []Derived{d}
	for _, x := range e.Derived {
		if x.MergeBase != d.MergeBase || x.Source != d.Source {
			out = append(out, x)
		}
	}
	if len(out) > 2 {
		out = out[:2]
	}
	e.Derived = out
}

// DerivedFor is the pair's data, if the entry has it.
func (e Entry) DerivedFor(mergeBase, source string) (Derived, bool) {
	for _, d := range e.Derived {
		if d.MergeBase == mergeBase && d.Source == source {
			return d, true
		}
	}
	return Derived{}, false
}

func (s *Store) entryPath(n int) string { return filepath.Join(s.root, "pr-"+strconv.Itoa(n)+".json") }
func (s *Store) listPath() string       { return filepath.Join(s.root, "list.json") }
func (s *Store) repoPath() string       { return filepath.Join(s.root, "repo.json") }

// LoadRepo reads the cached base repository.
func (s *Store) LoadRepo() (Repo, bool) {
	var r Repo
	return r, readJSON(s.repoPath(), &r)
}

// SaveRepo replaces the cached base repository.
func (s *Store) SaveRepo(r Repo) error {
	return s.locked(func() error { return writeJSON(s.repoPath(), r) })
}

// locked runs f under the directory's lock.
func (s *Store) locked(f func() error) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	release, err := filelock.Acquire(filepath.Join(s.root, "cache.lock"))
	if err != nil {
		return err
	}
	defer release()
	return f()
}

// readJSON decodes path into v; a corrupt file is quarantined and reads as
// absent, a missing one as absent.
func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		_ = os.Rename(path, path+".corrupt-"+strconv.FormatInt(time.Now().Unix(), 10))
		return false
	}
	return true
}

// writeJSON writes v to path atomically (temp + rename in the same dir).
func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// LoadList reads the cached listing.
func (s *Store) LoadList() (List, bool) {
	var l List
	return l, readJSON(s.listPath(), &l)
}

// SaveList replaces the cached listing.
func (s *Store) SaveList(l List) error {
	return s.locked(func() error { return writeJSON(s.listPath(), l) })
}

// Load reads PR n's entry.
func (s *Store) Load(n int) (Entry, bool) {
	var e Entry
	return e, readJSON(s.entryPath(n), &e)
}

// Save writes e and trims the directory to max entries, least recently
// opened first.
func (s *Store) Save(e Entry) error {
	return s.locked(func() error {
		if err := writeJSON(s.entryPath(e.Number), e); err != nil {
			return err
		}
		return s.trimLocked()
	})
}

// Update loads PR n's entry (a fresh one when absent), applies edit and
// saves it — all under the lock, so concurrent edits of one entry (this
// process or another) never lose each other.
func (s *Store) Update(n int, edit func(e *Entry)) error {
	return s.locked(func() error {
		e, ok := s.Load(n)
		if !ok {
			e = Entry{Number: n}
		}
		edit(&e)
		e.Number = n
		if err := writeJSON(s.entryPath(n), e); err != nil {
			return err
		}
		return s.trimLocked()
	})
}

// Remove deletes PR n's entry; an absent one is not an error.
func (s *Store) Remove(n int) error {
	return s.locked(func() error {
		if err := os.Remove(s.entryPath(n)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

func (s *Store) trimLocked() error {
	names, err := filepath.Glob(filepath.Join(s.root, "pr-*.json"))
	if err != nil || len(names) <= s.max {
		return err
	}
	type aged struct {
		path   string
		opened time.Time
	}
	var all []aged
	for _, p := range names {
		var e Entry
		if readJSON(p, &e) {
			all = append(all, aged{p, e.OpenedAt})
		}
	}
	slices.SortFunc(all, func(a, b aged) int { return b.opened.Compare(a.opened) })
	for _, a := range all[min(len(all), s.max):] {
		if err := os.Remove(a.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("prcache: trim %s: %w", filepath.Base(a.path), err)
		}
	}
	return nil
}

// numberOf parses "pr-<n>.json" (used by callers listing entries).
func numberOf(name string) (int, bool) {
	s, ok := strings.CutPrefix(filepath.Base(name), "pr-")
	if !ok {
		return 0, false
	}
	s, ok = strings.CutSuffix(s, ".json")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// Entries lists every cached entry, most recently opened first (prefetch
// walks it).
func (s *Store) Entries() []Entry {
	names, _ := filepath.Glob(filepath.Join(s.root, "pr-*.json"))
	var out []Entry
	for _, p := range names {
		if _, ok := numberOf(p); !ok {
			continue
		}
		var e Entry
		if readJSON(p, &e) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return b.OpenedAt.Compare(a.OpenedAt) })
	return out
}
