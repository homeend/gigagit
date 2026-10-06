package observ

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// clockAt is a settable now() for the weekly log.
type clockAt struct{ t time.Time }

func (c *clockAt) now() time.Time { return c.t }

// wed is a Wednesday noon in ISO week 2026-W41 (Mon 2026-10-05 … Sun 10-11).
var wed = time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		if !strings.HasSuffix(e.Name(), ".lock") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// writeAt appends line through l with the clock at t, then stamps the live
// file's mtime to t (the real clock wrote it "now").
func writeAt(t *testing.T, l *WeeklyLog, c *clockAt, at time.Time, line string) {
	t.Helper()
	c.t = at
	if _, err := l.Write([]byte(line)); err != nil {
		t.Fatalf("write %q: %v", line, err)
	}
	if err := os.Chtimes(l.path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestWeekStartIsMondayMidnight(t *testing.T) {
	t.Parallel()
	for _, in := range []time.Time{
		time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local), // Monday 00:00
		wed, // Wednesday
		time.Date(2026, 10, 11, 23, 59, 0, 0, time.Local), // Sunday night
	} {
		if got, want := weekStart(in), time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local); !got.Equal(want) {
			t.Errorf("weekStart(%v) = %v, want %v", in, got, want)
		}
	}
}

// A long-running process rolls over on its first write in a new week: last
// week's lines move to a dated archive, this week's land in a fresh file.
func TestWeeklyLogRollsOverOnFirstWriteOfANewWeek(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "errors.log")
	c := &clockAt{t: wed}
	l, err := openWeeklyLog(path, 4, c.now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	writeAt(t, l, c, wed, "old\n")
	writeAt(t, l, c, wed.AddDate(0, 0, 3), "sunday\n") // still W41
	writeAt(t, l, c, wed.AddDate(0, 0, 7), "new\n")    // W42

	if got := readFile(t, path); got != "new\n" {
		t.Errorf("live file = %q, want only this week's line", got)
	}
	if got := readFile(t, filepath.Join(dir, "errors-2026-W41.log")); got != "old\nsunday\n" {
		t.Errorf("archive = %q, want last week's lines", got)
	}
}

// A file left from an earlier week is rotated by the next process that writes.
func TestWeeklyLogRotatesAStaleFileOnOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "operations.log")
	if err := os.WriteFile(path, []byte("ancient\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := wed.AddDate(0, 0, -14) // W39
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	c := &clockAt{t: wed}
	l, err := openWeeklyLog(path, 4, c.now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	writeAt(t, l, c, wed, "fresh\n")
	if got := readFile(t, path); got != "fresh\n" {
		t.Errorf("live file = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "operations-2026-W39.log")); got != "ancient\n" {
		t.Errorf("archive = %q", got)
	}
}

// A current-week file is appended to, never rotated.
func TestWeeklyLogAppendsWithinTheWeek(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "errors.log")
	if err := os.WriteFile(path, []byte("monday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mon := time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local)
	if err := os.Chtimes(path, mon, mon); err != nil {
		t.Fatal(err)
	}
	c := &clockAt{t: wed}
	l, err := openWeeklyLog(path, 4, c.now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	writeAt(t, l, c, wed, "wednesday\n")
	if got := readFile(t, path); got != "monday\nwednesday\n" {
		t.Errorf("live file = %q", got)
	}
	if got := dirNames(t, dir); len(got) != 1 {
		t.Errorf("dir = %v, want only the live file", got)
	}
}

// Two processes share one log: the second to roll over finds the file the
// first already rotated (a current-week mtime) and must NOT archive it again
// — that would file this week's lines under last week.
func TestWeeklyLogSecondWriterDoesNotRotateAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "errors.log")
	ca, cb := &clockAt{t: wed}, &clockAt{t: wed}
	a, err := openWeeklyLog(path, 4, ca.now)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := openWeeklyLog(path, 4, cb.now)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	writeAt(t, a, ca, wed, "a-old\n")
	writeAt(t, b, cb, wed, "b-old\n")
	next := wed.AddDate(0, 0, 7)
	writeAt(t, a, ca, next, "a-new\n")
	writeAt(t, b, cb, next.Add(time.Minute), "b-new\n")

	if got := readFile(t, path); got != "a-new\nb-new\n" {
		t.Errorf("live file = %q, want both processes' new lines", got)
	}
	if got := readFile(t, filepath.Join(dir, "errors-2026-W41.log")); got != "a-old\nb-old\n" {
		t.Errorf("archive = %q", got)
	}
	if got := dirNames(t, dir); len(got) != 2 {
		t.Errorf("dir = %v, want the live file + one archive", got)
	}
}

// An archive name already taken is never overwritten.
func TestWeeklyLogNeverOverwritesAnArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "errors.log")
	taken := filepath.Join(dir, "errors-2026-W41.log")
	if err := os.WriteFile(taken, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &clockAt{t: wed}
	l, err := openWeeklyLog(path, 4, c.now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	writeAt(t, l, c, wed, "w41\n")
	writeAt(t, l, c, wed.AddDate(0, 0, 7), "w42\n")
	if got := readFile(t, taken); got != "keep me\n" {
		t.Errorf("existing archive = %q, was overwritten", got)
	}
	if got := readFile(t, filepath.Join(dir, "errors-2026-W41-2.log")); got != "w41\n" {
		t.Errorf("second archive = %q", got)
	}
}

// Only the newest `keep` archived weeks survive; other files in the state dir
// (and the other log's archives) are untouched.
func TestWeeklyLogPrunesToKeepWeeks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "errors.log")
	for _, n := range []string{
		"errors-2026-W30.log", "errors-2026-W35.log", "errors-2026-W36.log",
		"errors-2026-W37.log", "errors-2026-W38-2.log", "errors-2025-W52.log",
		"operations-2026-W30.log", "repos.toml", "errors-notes.log",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &clockAt{t: wed}
	l, err := openWeeklyLog(path, 4, c.now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	writeAt(t, l, c, wed, "w41\n")
	writeAt(t, l, c, wed.AddDate(0, 0, 7), "w42\n")
	want := []string{
		"errors-2026-W36.log", "errors-2026-W37.log", "errors-2026-W38-2.log",
		"errors-2026-W41.log", "errors-notes.log", "errors.log",
		"operations-2026-W30.log", "repos.toml",
	}
	if got := dirNames(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("dir =\n %v\nwant\n %v", got, want)
	}
}

// Close is idempotent-safe for the span sink (SetSpanSink closes a replaced
// sink) and later writes fail instead of reopening the file.
func TestWeeklyLogWriteAfterCloseFails(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "errors.log")
	c := &clockAt{t: wed}
	l, err := openWeeklyLog(path, 4, c.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("late\n")); err == nil {
		t.Error("write after Close succeeded")
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
