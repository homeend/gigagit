package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// prsendrows.js: the GitHub rows a note's right-click menu offers in a PR's
// own diff — the TUI's forgeNoteRows, row for row.
func TestPRSendRowsJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prsendrows.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prsendrows.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { sendRows } from "./prsendrows.mjs";
const ids = (rows) => rows.map((r) => r.id + ":" + r.label);
console.log(JSON.stringify({
  local: ids(sendRows({ id: "n1", source: "user", sync: "local", group: "mine" }, 7)),
  failed: ids(sendRows({ id: "n1", source: "user", sync: "failed", group: "mine" }, 7)),
  sending: ids(sendRows({ id: "n1", source: "user", sync: "sending", group: "mine" }, 7)),
  remark: ids(sendRows({ id: "review:r1:0", source: "agent", read_only: true, replyable: true, sync: "local", group: "review:r1" }, 7)),
  thread: ids(sendRows({ id: "forge:C1", source: "forge", read_only: true, replyable: true, sync: "github",
    replies: [{ id: "d1", source: "user" }, { id: "d2", source: "user" }, { id: "forge:C2", source: "forge" }] }, 7)),
  resolved: ids(sendRows({ id: "forge:C3", source: "forge", read_only: true, sync: "github", resolved: true }, 7)),
  reply: ids(sendRows({ id: "d1", parent_id: "forge:C1", source: "user", sync: "local" }, 7)),
  notPR: ids(sendRows({ id: "n1", source: "user", sync: "local", group: "mine" }, 0)),
}));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string][]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := map[string][]string{
		"local":    {"send:Send as GitHub comment", "send-review:Send my draft review…"},
		"failed":   {"send:Retry sending as GitHub comment", "send-review:Send my draft review…"},
		"sending":  {},
		"remark":   {"send:Send as GitHub comment", "send-review:Send this AI review…"},
		"thread":   {"reply-send:Reply & send…", "resolve:Resolve on GitHub", "send-drafts:Send 2 draft replies"},
		"resolved": {"reply-send:Reply & send…", "resolve:Reopen on GitHub"},
		"reply":    {},
		"notPR":    {},
	}
	for k, w := range want {
		if g := got[k]; !slices.Equal(g, w) && !(len(g) == 0 && len(w) == 0) {
			t.Errorf("%s = %q, want %q", k, g, w)
		}
	}
}

// prsend.js posts only to the send routes, names the PR by its number, and
// is wired into the page (the note menu hook, the module import).
func TestPRSendModuleIsWired(t *testing.T) {
	t.Parallel()
	read := func(f string) string {
		b, err := os.ReadFile(filepath.Join("static", f))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	js := read("prsend.js")
	for _, want := range []string{`"/api/pr/send?n=" + n`, `"/api/pr/send/groups?n=" + pr`, `registerRows("note"`, `registerRows("pr"`} {
		if !strings.Contains(js, want) {
			t.Errorf("prsend.js lacks %q", want)
		}
	}
	if !strings.Contains(read("app.js"), `import "./prsend.js";`) {
		t.Error("app.js does not import prsend.js")
	}
	if !strings.Contains(read("files.js"), `extraRows("note", {`) {
		t.Error("the note menu has no extraRows(\"note\") hook")
	}
	if m := read("menus.js"); !strings.Contains(m, `"note"`) || !strings.Contains(m, `"pr"`) {
		t.Error("menus.js does not know the note / pr menus")
	}
}

// Item 8: the verdict's typed body survives a refusal; the wiring of
// prkept.js (TestPRKeptJS holds the rule: only its own box's changing send
// drops a kept body).
func TestPRSendKeepsTheVerdictBody(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prsend.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`keptBody = { pr, group: "verdict", text };`,
		`keptBody = keptAfterSend(keptBody, n, body, ev);`,
		`keptText(keptBody, pr, "verdict")`,
		`fn(n, ev, body)`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("prsend.js lacks %q", want)
		}
	}
}
