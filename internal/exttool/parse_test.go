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
