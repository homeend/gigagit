package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// docs/specs/gg-steer/steer-cases.json: the replay corpus a steering consumer
// is built against (spec 2026-10-15-gg-steer-ide-consumer §7). gg's TUI
// answers every `sync` case that is not `ide_only` with the recorded reply;
// the async landings and the IDE's own refusals are documentation for the
// plugin, skipped here.
func TestSteerCases(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-steer", "steer-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name    string        `json:"name"`
			Sync    bool          `json:"sync"`
			IDEOnly bool          `json:"ide_only"`
			Command steer.Command `json:"command"`
			Reply   struct {
				OK          bool   `json:"ok"`
				Detail      string `json:"detail"`
				Error       string `json:"error"`
				ErrorPrefix string `json:"error_prefix"`
			} `json:"reply"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	base, dir := steerModel(t)
	base.ready = true
	ran := 0
	for _, c := range corpus.Cases {
		if !c.Sync || c.IDEOnly {
			continue
		}
		ran++
		c := c
		t.Run(c.Name, func(t *testing.T) {
			m := base
			m.attention = map[attentionKey][]steerMark{}
			_, cmd := m.applySteer(c.Command)
			runSteerCmd(t, cmd)
			r, ok := steer.AwaitReply(dir, c.Command.ID, time.Second)
			if !ok {
				t.Fatalf("no reply (an async case marked sync?)")
			}
			if r.OK != c.Reply.OK {
				t.Fatalf("reply = %+v, want ok=%v", r, c.Reply.OK)
			}
			if c.Reply.Error != "" && r.Error != c.Reply.Error {
				t.Fatalf("error = %q, want %q", r.Error, c.Reply.Error)
			}
			if c.Reply.ErrorPrefix != "" && !strings.HasPrefix(r.Error, c.Reply.ErrorPrefix) {
				t.Fatalf("error = %q, want prefix %q", r.Error, c.Reply.ErrorPrefix)
			}
			if c.Reply.Detail != "" && r.Detail != c.Reply.Detail {
				t.Fatalf("detail = %q, want %q", r.Detail, c.Reply.Detail)
			}
		})
	}
	if ran < 5 {
		t.Fatalf("only %d sync cases ran", ran)
	}
}
