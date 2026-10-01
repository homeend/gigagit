package e2e

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/tui"
)

var update = flag.Bool("update", false, "rewrite the TUI golden screens of the scenarios run (read every changed one before committing)")

// runTUI drives the scenario's [tui] steps against the built sandbox and
// compares every named checkpoint with <scenario>.screens/NN-<name>.txt.
func runTUI(t *testing.T, sb *Sandbox, sc *Scenario, file string) {
	t.Helper()
	if *update {
		if err := updateRefused(runtime.GOOS); err != nil {
			t.Fatal(err)
		}
	}
	w, h := parseSize(sc.TUI.Size)
	svc := domain.OpenTUI(sb.LocalDir)
	hd, err := tui.NewHeadless(svc, tui.HeadlessOptions{Width: w, Height: h, StatePath: filepath.Join(sb.Root, "state", "repos.toml")})
	if err != nil {
		t.Fatalf("tui start: %v", err)
	}
	defer hd.Close()
	dir := strings.TrimSuffix(file, ".toml") + ".screens"
	wanted := map[string]bool{}
	ord := 0
	var keys []string
	for i, st := range sc.TUI.Steps {
		for _, k := range st.Keys {
			if err := hd.Press(k); err != nil {
				t.Fatalf("tui step %d (%s) key %q: %v", i, st.Name, k, err)
			}
			keys = append(keys, k)
		}
		if st.Wait {
			if err := hd.FireTimers(); err != nil {
				t.Fatalf("tui step %d (%s) wait: %v", i, st.Name, err)
			}
			keys = append(keys, "<wait>")
		}
		screen := normalizeRoot(hd.Screen(), sb.Root)
		for _, s := range st.ScreenContains {
			if !strings.Contains(screen, s) {
				t.Fatalf("tui step %d (%s): screen lacks %q\n%s", i, st.Name, s, screen)
			}
		}
		for _, s := range st.ScreenExcludes {
			if strings.Contains(screen, s) {
				t.Fatalf("tui step %d (%s): screen contains %q\n%s", i, st.Name, s, screen)
			}
		}
		if st.Name == "" {
			continue
		}
		ord++
		golden := filepath.Join(dir, fmt.Sprintf("%02d-%s.txt", ord, st.Name))
		wanted[filepath.Base(golden)] = true
		compareGolden(t, golden, screen, st.Name, keys)
	}
	checkStaleGoldens(t, dir, wanted)
}

// parseSize reads a validated "COLSxROWS" ("" = 160x40).
func parseSize(s string) (int, int) {
	if s == "" {
		return 160, 40
	}
	c, r, _ := strings.Cut(s, "x")
	w, _ := strconv.Atoi(c)
	h, _ := strconv.Atoi(r)
	return w, h
}

// normalizeRoot replaces the sandbox root with {{root}}, padded or cut to
// the root's own width, so the rest of each row keeps its columns.
func normalizeRoot(screen, root string) string {
	ph := "{{root}}"
	n := len([]rune(root))
	switch {
	case len(ph) < n:
		ph += strings.Repeat("_", n-len(ph))
	case len(ph) > n:
		ph = ph[:n]
	}
	screen = strings.ReplaceAll(screen, root, ph)
	// A cell elided through the root (a middle-cut path) keeps a raw prefix
	// with the per-process pid segment (tuiRoot): mask its digits in place.
	return pidSegment.ReplaceAllStringFunc(screen, func(m string) string {
		return "gg-tui-" + strings.Repeat("#", len(m)-len("gg-tui-"))
	})
}

// pidSegment is tuiRoot's per-process directory name, whole or cut.
var pidSegment = regexp.MustCompile(`gg-tui-[0-9]{1,8}`)

// updateRefused refuses -update on Windows: its screens show Windows paths,
// and goldens are compared only elsewhere.
func updateRefused(goos string) error {
	if goos == "windows" {
		return fmt.Errorf("-update is refused on windows: its screens show windows paths; write goldens on linux or macOS")
	}
	return nil
}

// compareGolden checks a checkpoint's screen against its golden file, or
// writes it under -update. A mismatch writes <golden>.actual beside it.
func compareGolden(t *testing.T, golden, screen, name string, keys []string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(screen+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(golden + ".actual")
		return
	}
	if runtime.GOOS == "windows" {
		return // paths differ there; contains/excludes already ran (spec §4)
	}
	b, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("checkpoint %q: no golden %s (run with -update, then READ it before committing)", name, golden)
	}
	want := strings.TrimSuffix(string(b), "\n")
	if want == screen {
		_ = os.Remove(golden + ".actual")
		return
	}
	_ = os.WriteFile(golden+".actual", []byte(screen+"\n"), 0o644)
	t.Fatalf("checkpoint %q (keys %v) differs from %s (actual written beside it):\n%s", name, keys, golden, lineDiff(want, screen))
}

// checkStaleGoldens fails on a golden no checkpoint produces (a renamed or
// removed step), or deletes it under -update.
func checkStaleGoldens(t *testing.T, dir string, wanted map[string]bool) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	for _, f := range files {
		if wanted[filepath.Base(f)] {
			continue
		}
		if *update {
			_ = os.Remove(f)
			continue
		}
		t.Fatalf("stale golden %s: no checkpoint produces it (rename it, or run -update)", f)
	}
}

// lineDiff lists each differing row: "-" golden, "+" actual.
func lineDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	var out strings.Builder
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			fmt.Fprintf(&out, "row %d\n- %s\n+ %s\n", i+1, x, y)
		}
	}
	return out.String()
}
