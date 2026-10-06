package observ

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/filelock"
)

// LogKeepWeeks is how many archived weeks a WeeklyLog keeps beside the live
// file; older archives are deleted at rollover.
const LogKeepWeeks = 4

// rotateRetry is how long a failed rollover (Windows: another gg process still
// holds the file open, so it cannot be renamed) waits before the next try.
// Writes keep appending to the live file meanwhile — a log line is never lost
// to rotation.
const rotateRetry = time.Hour

// WeeklyLog is an append-only log file that rolls over once per calendar week
// (Monday 00:00 local, the ISO week): the first write in a new week moves the
// live file to <stem>-<YYYY>-W<ww><ext> (the ISO week of its last write) and
// starts a fresh one. Several gg processes may append to the same file — the
// TUI, gg web, other TUIs — so a rollover runs under a cross-process lock and
// only archives a file whose last write was BEFORE this week: the second
// process to roll over finds the first one's fresh file and just reopens it.
type WeeklyLog struct {
	mu    sync.Mutex
	path  string
	keep  int
	now   func() time.Time
	f     *os.File
	week  time.Time // start of the week the open file belongs to
	retry time.Time // a failed rollover is not retried before this
}

// OpenWeeklyLog opens (creating it and its directory as needed) the weekly
// log at path, keeping LogKeepWeeks archived weeks.
func OpenWeeklyLog(path string) (*WeeklyLog, error) {
	return openWeeklyLog(path, LogKeepWeeks, time.Now)
}

func openWeeklyLog(path string, keep int, now func() time.Time) (*WeeklyLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	l := &WeeklyLog{path: path, keep: keep, now: now, week: weekStart(now())}
	// A file last written in an earlier week belongs to that week: the first
	// write rolls it over (lazily, so a process that never logs never renames).
	if fi, err := os.Stat(path); err == nil && fi.ModTime().Before(l.week) {
		l.week = weekStart(fi.ModTime())
	}
	f, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	l.f = f
	return l, nil
}

// Write appends p, rolling the file over first when the week has turned.
func (l *WeeklyLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, os.ErrClosed
	}
	if t := l.now(); weekStart(t).After(l.week) && !t.Before(l.retry) {
		l.roll(t)
		if l.f == nil {
			return 0, os.ErrClosed
		}
	}
	return l.f.Write(p)
}

// Close closes the live file. Later writes fail; a second Close is a no-op.
func (l *WeeklyLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// roll closes the live handle (Windows cannot rename an open file), archives
// the file when it still holds an earlier week, and reopens the path. A
// failed archive keeps appending to the old file and retries later.
func (l *WeeklyLog) roll(t time.Time) {
	_ = l.f.Close()
	l.f = nil
	if err := l.archive(weekStart(t)); err != nil {
		l.retry = t.Add(rotateRetry)
	} else {
		l.week = weekStart(t)
	}
	if f, err := openAppend(l.path); err == nil {
		l.f = f
	}
}

// archive moves the live file to its dated archive name when its last write
// predates week, then prunes old archives. Under the cross-process lock the
// mtime check is what makes a concurrent rollover a no-op.
func (l *WeeklyLog) archive(week time.Time) error {
	release, err := filelock.Acquire(l.path + ".lock")
	if err != nil {
		return err
	}
	defer release()
	fi, err := os.Stat(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.ModTime().Before(week) {
		return nil // another process already rolled over
	}
	dst, err := l.archiveName(fi.ModTime())
	if err != nil {
		return err
	}
	if err := os.Rename(l.path, dst); err != nil {
		return err
	}
	l.prune()
	return nil
}

// archiveName is <stem>-<YYYY>-W<ww><ext> for the ISO week of t, with -2, -3…
// appended when that name is taken: an archive is never overwritten.
func (l *WeeklyLog) archiveName(t time.Time) (string, error) {
	stem, ext := l.stemExt()
	y, w := t.ISOWeek()
	base := fmt.Sprintf("%s-%04d-W%02d", stem, y, w)
	for i := 1; i < 1000; i++ {
		name := base + ext
		if i > 1 {
			name = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		if _, err := os.Lstat(name); errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
	}
	return "", fmt.Errorf("no free archive name for %s", base+ext)
}

func (l *WeeklyLog) stemExt() (stem, ext string) {
	ext = filepath.Ext(l.path)
	return strings.TrimSuffix(l.path, ext), ext
}

// prune deletes the archives of all but the newest l.keep weeks. Best effort:
// a file that cannot be removed now is removed at a later rollover.
func (l *WeeklyLog) prune() {
	stem, ext := l.stemExt()
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(filepath.Base(stem)) +
		`-(\d{4}-W\d{2})(?:-\d+)?` + regexp.QuoteMeta(ext) + `$`)
	entries, err := os.ReadDir(filepath.Dir(l.path))
	if err != nil {
		return
	}
	byWeek := map[string][]string{}
	var weeks []string
	for _, e := range entries {
		m := re.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		if _, seen := byWeek[m[1]]; !seen {
			weeks = append(weeks, m[1])
		}
		byWeek[m[1]] = append(byWeek[m[1]], e.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(weeks))) // newest first: YYYY-Www sorts lexically
	for i := l.keep; i < len(weeks); i++ {
		for _, name := range byWeek[weeks[i]] {
			_ = os.Remove(filepath.Join(filepath.Dir(l.path), name))
		}
	}
}

// weekStart is Monday 00:00 of t's week in t's location.
func weekStart(t time.Time) time.Time {
	back := (int(t.Weekday()) + 6) % 7 // days since Monday
	y, m, d := t.Date()
	return time.Date(y, m, d-back, 0, 0, 0, 0, t.Location())
}
