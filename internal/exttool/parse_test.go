package exttool

import "testing"

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
