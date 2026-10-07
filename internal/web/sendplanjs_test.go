package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sendplan.js draws the GitHub send confirm from the plan the server built:
// every item (capped, "+ N more"), every skip with its reason, the body
// escaped; its buttons are the decision's options, worded.
func TestSendPlanJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "sendplan.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sendplan.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { sendConfirmHTML, sendDecision, sendOptionLabel, SEND_CONFIRM_CAP } from "./sendplan.mjs";
const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;");
const items = Array.from({ length: 40 }, (_, i) => ({ label: "a.go:" + (i + 1) + " remark " + i }));
items[0].replies = 2;
const plan = { target: "o/r #7", mode: "review", verdict: true, body: "line1\n<b>x</b>", items,
  skipped: [{ label: "b.go:3 old", reason: "its lines changed" }] };
const html = sendConfirmHTML(plan, esc);
const ev = sendDecision({ id: "forge.send", prompt: "Send to …", options: ["comment", "approve", "request-changes", "abort"] }, plan, esc);
const finish = sendConfirmHTML({ target: "o/r #7", mode: "finish", has_pending: true, items: [{ label: "k1" }], skipped: [] }, esc);
console.log(JSON.stringify({ html, finish, labels: ev.labels, evhtml: ev.html === html, cap: SEND_CONFIRM_CAP,
  pending: sendOptionLabel("submit-with-pending"), abort: sendOptionLabel("abort"), odd: sendOptionLabel("odd") }));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		HTML, Finish        string
		Labels              map[string]string
		EvHTML              bool
		Cap                 int
		Pending, Abort, Odd string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, want := range []string{"o/r #7", "&lt;b>x", "remark 0", "(+ 2 replies)", "remark 29", "+ 10 more", "b.go:3 old", "its lines changed", "40 items"} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("confirm lacks %q:\n%s", want, got.HTML)
		}
	}
	if strings.Contains(got.HTML, "remark 30") || strings.Contains(got.HTML, "<b>x") {
		t.Errorf("not capped or not escaped:\n%s", got.HTML)
	}
	if !strings.Contains(got.Finish, "interrupted send") || strings.Contains(got.Finish, "join it") {
		t.Errorf("a finish confirm says what it finishes, and nothing about joining:\n%s", got.Finish)
	}
	if got.Labels["request-changes"] != "Request changes" || got.Labels["abort"] != "Cancel" || got.Labels["comment"] != "Comment" || !got.EvHTML || got.Cap != 30 {
		t.Errorf("labels = %v evhtml %v cap %d", got.Labels, got.EvHTML, got.Cap)
	}
	if got.Pending != "Submit with my pending review" || got.Abort != "Cancel" || got.Odd != "odd" {
		t.Errorf("pending %q abort %q odd %q", got.Pending, got.Abort, got.Odd)
	}
}

// ops.js shows a forge.send decision through the plan its send kept, and
// the modal paints html + labels when an event carries them.
func TestOpsShowsTheSendConfirmFromThePlan(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "ops.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{
		`plan && ev.id === "forge.send" ? sendDecision(ev, plan, esc) : ev`,
		`if (ev.html) $("modal-prompt").innerHTML = ev.html;`,
		`(ev.labels && ev.labels[o]) || o`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("ops.js lacks %q", want)
		}
	}
}
