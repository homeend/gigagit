package exttool

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
)

var updateGolden = flag.Bool("update", false, "rewrite catalog_versions.golden (refuses un-bumped changes)")

// familyPrints maps "<tool>\t<category>\t<name>" to the family's Version and
// a fingerprint of all its variants in catalog order (Range and OptIn
// included) — the normalised meaning, so a whitespace-only edit needs no bump.
func familyPrints() map[string][2]string {
	out := map[string][2]string{}
	for _, tl := range Builtins() {
		parts := map[string][]string{}
		vers := map[string]int{}
		for _, ct := range tl.Commands {
			k := tl.ID + "\t" + string(ct.Category) + "\t" + ct.Name
			fp := config.ToolFingerprint(config.ToolCommand{Mode: string(ct.Mode), PerFile: ct.PerFile, WhenOp: ct.WhenOp, Frontends: ct.Frontends, Command: ct.Command})
			parts[k] = append(parts[k], fmt.Sprintf("%s=%s;optin=%t", ct.Range, fp, ct.OptIn))
			vers[k] = ct.Version
		}
		for k, p := range parts {
			out[k] = [2]string{strconv.Itoa(vers[k]), strings.Join(p, "|")}
		}
	}
	return out
}

// TestCatalogVersionBumpGuard fails when a catalog family's template changes
// without its Version being raised — the only way stamped config blocks
// learn a newer template exists.
func TestCatalogVersionBumpGuard(t *testing.T) {
	path := filepath.Join("testdata", "catalog_versions.golden")
	prev := map[string][2]string{}
	if raw, err := os.ReadFile(path); err == nil {
		for _, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			f := strings.Split(ln, "\t")
			if len(f) == 5 {
				prev[f[0]+"\t"+f[1]+"\t"+f[2]] = [2]string{f[3], f[4]}
			}
		}
	} else if !*updateGolden {
		t.Fatalf("%s missing: run go test ./internal/exttool -run TestCatalogVersionBumpGuard -update", path)
	}
	cur := familyPrints()
	for k, c := range cur {
		p, ok := prev[k]
		if !ok {
			continue // a new family
		}
		pv, _ := strconv.Atoi(p[0])
		cv, _ := strconv.Atoi(c[0])
		switch {
		case cv < pv:
			t.Errorf("%s: Version went down %d→%d", strings.ReplaceAll(k, "\t", " / "), pv, cv)
		case p[1] != c[1] && cv == pv:
			t.Errorf("%s: template changed but Version stayed %d — bump it", strings.ReplaceAll(k, "\t", " / "), cv)
		}
	}
	if !*updateGolden {
		for k, c := range cur {
			if p, ok := prev[k]; !ok || p != c {
				t.Errorf("%s: golden is stale — run with -update", strings.ReplaceAll(k, "\t", " / "))
			}
		}
		return
	}
	if t.Failed() {
		return
	}
	keys := make([]string, 0, len(cur))
	for k := range cur {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", k, cur[k][0], cur[k][1])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
