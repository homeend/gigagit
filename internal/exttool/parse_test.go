package exttool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseCaptureMessageStripsCodeFence(t *testing.T) {
	// Raw plain fenced output (Claude run without --output-format json).
	if s, b, err := ParseCaptureMessage([]byte("```\nFix the thing\n\nBecause reasons.\n```")); err != nil || s != "Fix the thing" || b != "Because reasons." {
		t.Fatalf("raw fence: (%q,%q,%v)", s, b, err)
	}
	// Fence with a language tag, no body.
	if s, _, err := ParseCaptureMessage([]byte("```text\nFix the thing\n```")); err != nil || s != "Fix the thing" {
		t.Fatalf("lang fence: (%q,%v)", s, err)
	}
	// Fenced content inside a JSON .result envelope (the real Claude case reported).
	env, _ := json.Marshal(map[string]any{"is_error": false, "result": "```\nFix the thing\n\nBody.\n```"})
	if s, b, err := ParseCaptureMessage(env); err != nil || s != "Fix the thing" || b != "Body." {
		t.Fatalf("json fence: (%q,%q,%v)", s, b, err)
	}
	// A ``` that only appears inside the body (does not open the message) is preserved.
	if s, b, err := ParseCaptureMessage([]byte("Add fenced example\n\nUse ```go blocks.")); err != nil || s != "Add fenced example" || !strings.Contains(b, "```go") {
		t.Fatalf("mid-body fence must be preserved: (%q,%q,%v)", s, b, err)
	}
}

func TestParseCaptureReport(t *testing.T) {
	// Claude --output-format json wraps the markdown report in .result.
	if got, err := ParseCaptureReport(`{"result":"## Review\n- finding","is_error":false}`); err != nil || got != "## Review\n- finding" {
		t.Fatalf("claude envelope: (%q,%v)", got, err)
	}
	// A tool-reported error surfaces as err, using .result as the message.
	if _, err := ParseCaptureReport(`{"is_error":true,"result":"boom"}`); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("is_error: err=%v, want it to contain %q", err, "boom")
	}
	// An is_error envelope with no usable .result falls back to a generic message.
	if _, err := ParseCaptureReport(`{"is_error":true}`); err == nil || !strings.Contains(err.Error(), "error") {
		t.Fatalf("is_error no result: err=%v", err)
	}
	// Raw markdown (Junie's $GG_MESSAGE_FILE path) passes through unchanged,
	// code fences and all — a report is never subject/body split or defenced.
	raw := "### Summary\n- did a thing\n\n```go\nfunc f() {}\n```"
	if got, err := ParseCaptureReport(raw); err != nil || got != raw {
		t.Fatalf("raw markdown: (%q,%v) want unchanged %q", got, err, raw)
	}
	// Plain text passes through unchanged too.
	if got, err := ParseCaptureReport("just a plain report"); err != nil || got != "just a plain report" {
		t.Fatalf("plain text: (%q,%v)", got, err)
	}
	// Whitespace-only input is trimmed to empty, not an error (the caller,
	// domain.ReviewReport, treats an empty report as its own failure).
	if got, err := ParseCaptureReport("   \n  "); err != nil || got != "" {
		t.Fatalf("whitespace: (%q,%v)", got, err)
	}
	// JSON-looking but no "result" key: falls through to raw-text handling.
	if got, err := ParseCaptureReport(`{"foo":"bar"}`); err != nil || got != `{"foo":"bar"}` {
		t.Fatalf("no-result json: (%q,%v)", got, err)
	}
	// Malformed JSON falls through to raw-text handling too.
	if got, err := ParseCaptureReport("{not valid json"); err != nil || got != "{not valid json" {
		t.Fatalf("malformed json: (%q,%v)", got, err)
	}
}

func TestParseCaptureMessage(t *testing.T) {
	cases := []struct {
		name, in, subj, body string
		wantErr              bool
	}{
		{"claude_plain", "Fix the thing\n\nBecause reasons.", "Fix the thing", "Because reasons.", false},
		{"claude_json_result", `{"type":"result","is_error":false,"result":"Fix the thing\n\nBecause reasons."}`, "Fix the thing", "Because reasons.", false},
		{"claude_structured", `{"type":"result","is_error":false,"result":"{\"subject\":\"S\"}","structured_output":{"subject":"Add cap","body":"Bound the diff."}}`, "Add cap", "Bound the diff.", false},
		{"is_error", `{"type":"result","is_error":true,"result":"tool blew up"}`, "", "", true},
		{"junie_report", "{\"result\":\"### Summary\\n- did a thing\\n\\n### Changes\\n- x\"}", "### Summary", "- did a thing\n\n### Changes\n- x", false},
		{"top_level_subject", `{"subject":"Direct","body":"B"}`, "Direct", "B", false},
		{"garbage_nonjson", "just text here", "just text here", "", false},
		{"empty", "   \n  ", "", "", true},
		{"empty_result", `{"type":"result","is_error":false,"result":""}`, "", "", true},
		{"malformed_json", "{not valid json", "{not valid json", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			subj, body, err := ParseCaptureMessage([]byte(c.in))
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if err == nil && (subj != c.subj || body != c.body) {
				t.Fatalf("got (%q,%q) want (%q,%q)", subj, body, c.subj, c.body)
			}
		})
	}
}
