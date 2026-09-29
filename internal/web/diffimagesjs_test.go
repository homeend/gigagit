package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const diffImagesPureStart = "// --- diffimages pure (guarded against Go) ---"
const diffImagesPureEnd = "// --- end diffimages pure ---"

// runDiffImagesPure evaluates diffimages.js's pure section — preceded by
// core.js's fmtBytes, the one import it uses — then body (which must
// console.log its answers), under node.
func runDiffImagesPure(t *testing.T, body string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	core := staticSrc(t, "core.js")
	k := strings.Index(core, "export function fmtBytes(")
	e := strings.Index(core[k:], "\n}\n")
	if k < 0 || e < 0 {
		t.Fatalf("core.js: fmtBytes is gone")
	}
	fmtBytes := strings.TrimPrefix(core[k:k+e+3], "export ")
	src := staticSrc(t, "diffimages.js")
	i, j := strings.Index(src, diffImagesPureStart), strings.Index(src, diffImagesPureEnd)
	if i < 0 || j < i {
		t.Fatalf("diffimages.js: the pure section markers are gone")
	}
	script := filepath.Join(t.TempDir(), "t.mjs")
	if err := os.WriteFile(script, []byte(fmtBytes+"\n"+src[i:j]+"\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestImagePairHTML(t *testing.T) {
	t.Parallel()
	got := runDiffImagesPure(t, `
const two = { url: "/api/diff?sha=a&path=p.png", images: { old: { kind: "png", width: 8, height: 4, size: 1234 }, new: { kind: "png", width: 16, height: 8, size: 2048 } } };
const added = { url: "/api/diff?sha=a&path=p.png", images: { new: { kind: "gif", width: 3, height: 3, size: 10 } } };
console.log(imgInfo(two.images.old));
console.log(nextLayout("side"), nextLayout("stacked"), nextLayout("single"));
const side = imagePairHTML(two, "side", false);
console.log(side.includes('class="dimg-chips"'), (side.match(/<img /g) || []).length, side.includes("old: png 8×4"), side.includes("&amp;img=old"), side.includes("dimg-cols"));
const stacked = imagePairHTML(two, "stacked", false);
console.log((stacked.match(/<img /g) || []).length, stacked.includes("dimg-stacked"));
const single = imagePairHTML(two, "single", false);
console.log((single.match(/<img /g) || []).length, single.includes("new: png 16×8"), single.includes("tab"), single.includes("dimg-flip"));
console.log(imagePairHTML(two, "single", true).includes("old: png 8×4"));
const one = imagePairHTML(added, "side", false);
console.log(one.includes("dimg-chips"), one.includes("new:"), one.includes("gif 3×3"), (one.match(/<img /g) || []).length, one.includes("dimg-flip"));
console.log(hasImagePair(two), hasImagePair(added), hasImages(added), hasImages({ binary: true }));
const stk = stackImageHTML(two);
console.log(stk.includes("dimg-stk"), stk.includes("dimg-chips"), (stk.match(/<img /g) || []).length);
const stkOne = stackImageHTML(added);
console.log((stkOne.match(/<img /g) || []).length, stkOne.includes("new:"));
`)
	want := strings.Join([]string{
		"png 8×4, 1.2 KB",
		"stacked single side",
		"true 2 true true true",
		"2 true",
		"1 true true true",
		"true",
		"false false true 1 false",
		"true false true false",
		"true false 2",
		"1 false",
	}, "\n")
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
