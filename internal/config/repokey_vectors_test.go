package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// docs/specs/gg-steer/repokey-vectors.json: EncodeRepoKey's inputs and
// outputs a reader of the steering inbox reproduces (spec
// 2026-10-15-gg-steer-ide-consumer §2). Rows with an "os" run only there.
func TestRepoKeyVectors(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-steer", "repokey-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		In, Out, OS string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, c := range cases {
		if c.OS != "" && c.OS != runtime.GOOS {
			continue
		}
		ran++
		if got := EncodeRepoKey(c.In); got != c.Out {
			t.Errorf("EncodeRepoKey(%q) = %q, want %q", c.In, got, c.Out)
		}
	}
	if ran == 0 {
		t.Fatal("no vector ran on this platform")
	}
}
