package cli

import (
	"reflect"
	"testing"
)

func TestPushRejectPolicy(t *testing.T) {
	cases := map[string]map[string]string{
		"rebase":           {"push-rejected": "rebase"},
		"force":            {"push-rejected": "force", "push-force": "force"},
		"force-with-lease": {"push-rejected": "force", "push-force": "force-with-lease"},
		"abort":            {"push-rejected": "abort"},
		// Unset leaves push-rejected unanswered: a non-interactive rejected push
		// then fails fast (cliDecider errors); an interactive one prompts.
		"": {},
	}
	for in, want := range cases {
		got, err := pushRejectPolicy(in)
		if err != nil {
			t.Fatalf("pushRejectPolicy(%q) err=%v", in, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("pushRejectPolicy(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPushRejectPolicyRejectsUnknown(t *testing.T) {
	if _, err := pushRejectPolicy("merge"); err == nil {
		t.Fatal("unknown --on-reject value must error")
	}
}
