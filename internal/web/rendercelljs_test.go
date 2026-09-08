package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// renderCell writes the syntax-class suffix straight into a class attribute,
// so the value must be constrained to the shape syntax.Class.String() emits
// ("kw", "str", …). This guard runs the real function under node — esc and
// runes are stubbed, everything else is the shipped source — and checks that
// an off-shape class is dropped rather than interpolated, and that dropping it
// leaves the surrounding text as ONE plain run (the filter runs while the mask
// is filled, not at emit time).
func TestRenderCellWhitelistsClassJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i := strings.Index(s, "function renderCell(")
	if i < 0 {
		t.Fatal("files.js: renderCell is gone")
	}
	j := strings.Index(s[i:], "\n}\n")
	if j < 0 {
		t.Fatal("files.js: renderCell has no closing brace at column 0")
	}
	fn := s[i : i+j+2]

	cases := []struct {
		name string
		text string
		toks [][3]any
		want string
	}{
		{"good class wraps", "var x", [][3]any{{0, 3, "kw"}}, `<span class="tk-kw">var</span> x`},
		{"attribute break dropped", "var x", [][3]any{{0, 3, `kw" onload=alert(1) x="`}}, "var x"},
		{"too long dropped", "var x", [][3]any{{0, 3, "keyword"}}, "var x"},
		{"uppercase dropped", "var x", [][3]any{{0, 3, "KW"}}, "var x"},
		{"non-string dropped", "var x", [][3]any{{0, 3, 7}}, "var x"},
		{"dropped run merges with its neighbours", "abcdef", [][3]any{{0, 2, "zzzz"}, {2, 4, "kw"}}, `ab<span class="tk-kw">cd</span>ef`},
	}

	script := "const esc = (x) => String(x).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\"/g, '&quot;');\n" +
		"const runes = (x) => [...x];\n" + fn +
		"\nconst cases = JSON.parse(process.argv[1]);\n" +
		"console.log(JSON.stringify(cases.map((c) => renderCell(c[0], null, c[1], 'r'))));\n"
	arg, _ := json.Marshal(func() []any {
		var all []any
		for _, c := range cases {
			all = append(all, []any{c.text, c.toks})
		}
		return all
	}())
	cmd := exec.Command(node, "-e", script, string(arg))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("bad node output %q: %v", out, err)
	}
	for n, c := range cases {
		if got[n] != c.want {
			t.Errorf("%s: renderCell = %q, want %q", c.name, got[n], c.want)
		}
	}
}
