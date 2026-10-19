package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// The web answers the corpus's retired cases with the recorded refusal
// (HTTP 200, ok=false), so an old gg session CLI prints the fix.
func TestWebAnswersRetiredSteerVerbs(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-steer", "steer-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name    string          `json:"name"`
			Retired bool            `json:"retired"`
			Command json.RawMessage `json:"command"`
			Reply   struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			} `json:"reply"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	s, _, _ := attachedSrv(t)
	ran := 0
	for _, c := range corpus.Cases {
		if !c.Retired {
			continue
		}
		ran++
		code, r := steerAsk(t, s, string(c.Command))
		if code != http.StatusOK || r.OK != c.Reply.OK || r.Error != c.Reply.Error {
			t.Errorf("%s: code %d reply %+v, want error %q", c.Name, code, r, c.Reply.Error)
		}
	}
	if ran < 2 {
		t.Fatalf("only %d retired cases", ran)
	}
	var _ steer.Reply
}
