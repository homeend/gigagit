package config

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// The TUI and the web page it hosts write the same config from one process:
// concurrent tool-block writes must never lose one another's change.
func TestToolWritersDoNotLoseConcurrentUpdates(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	seed := ToolCommand{Category: "review", Name: "Seed", Mode: "capture", Command: "seed"}
	if err := AppendToolCommands(path, []ToolCommand{seed}); err != nil {
		t.Fatal(err)
	}
	const n = 24
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tc := ToolCommand{Category: "review", Name: fmt.Sprintf("T%d", i), Mode: "capture", Command: "x"}
			if err := AppendToolCommands(path, []ToolCommand{tc}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		nb := seed
		nb.Command, nb.TemplateVersion = "seed2", 2
		if _, err := ReplaceToolCommand(path, seed.Key(), nb); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	got, err := ToolCommandsIn(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n+1 {
		t.Fatalf("%d blocks survived, want %d (a concurrent write was lost)", len(got), n+1)
	}
}
