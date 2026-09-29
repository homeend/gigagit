package config

import (
	"fmt"
	"os"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ReplaceToolCommand rewrites, in place, the one [[tools.command]] block
// whose (category, name) is key with tc, leaving every other byte of the
// file alone and keeping its line ending. A block spans from its header to
// the line before the next table header (a "[" line inside a multi-line
// literal is body, not a header), minus trailing blank and comment lines —
// they belong to what follows. false, nil when no block has that key.
func ReplaceToolCommand(path, key string, tc ToolCommand) (bool, error) {
	if strings.Contains(tc.Command, "'''") {
		return false, fmt.Errorf("config: %s: command must not contain ''' (TOML literal delimiter)", tc.Name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	nl := "\n"
	if strings.Contains(string(raw), "\r\n") {
		nl = "\r\n"
	}
	lines := strings.SplitAfter(string(raw), "\n")
	var starts []int
	inLiteral := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case inLiteral:
			if strings.TrimRight(l, "\r\n") == "'''" {
				inLiteral = false
			}
		case strings.HasPrefix(t, "["):
			starts = append(starts, i)
		case isCommandLiteralOpen(t):
			inLiteral = true
		}
	}
	for si, s := range starts {
		if strings.TrimSpace(lines[s]) != "[[tools.command]]" {
			continue
		}
		end := len(lines)
		if si+1 < len(starts) {
			end = starts[si+1]
		}
		for end > s+1 {
			t := strings.TrimSpace(lines[end-1])
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			end--
		}
		var one struct {
			Tools ToolsConfig `toml:"tools"`
		}
		if err := toml.Unmarshal([]byte(strings.Join(lines[s:end], "")), &one); err != nil || len(one.Tools.Command) != 1 {
			continue
		}
		if one.Tools.Command[0].Key() != key {
			continue
		}
		block := strings.ReplaceAll(RenderToolCommand(tc), "\n", nl)
		out := strings.Join(lines[:s], "") + block + strings.Join(lines[end:], "")
		return true, atomicWriteFile(path, []byte(out))
	}
	return false, nil
}
