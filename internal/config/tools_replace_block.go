package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ErrToolBlockNotFound: the file holds no [[tools.command]] block with the
// key (or none gg can safely rewrite).
var ErrToolBlockNotFound = errors.New("config: tool block not found")

// ReplaceToolCommand rewrites, in place, the one [[tools.command]] block
// whose (category, name) is key with tc, keeping the file's line ending and
// every byte outside the block (with the key twice, the last — effective —
// block is the one rewritten). A block spans from its header to the line
// before the next table header, minus trailing blank and comment lines
// (they belong to what follows). Headers are found by a TOML-aware scan
// (multi-line strings, quoted values and comments are skipped), and the
// result is checked before it is written: everything outside the block
// must parse back identical, or nothing is written. false, nil when no
// block has that key.
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
	starts, tools := tomlHeaders(lines)
	// Walk from the end: with the same key twice, the LAST block is the
	// effective one (the overlay rule) and the one a status describes.
	for si := len(starts) - 1; si >= 0; si-- {
		s := starts[si]
		if !tools[si] {
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
		index := 0 // this block's position among the file's tool blocks
		for j := 0; j < si; j++ {
			if tools[j] {
				index++
			}
		}
		block := strings.ReplaceAll(RenderToolCommand(tc), "\n", nl)
		out := strings.Join(lines[:s], "") + block + strings.Join(lines[end:], "")
		if err := sameOutsideToolBlock(raw, []byte(out), index); err != nil {
			return false, err
		}
		return true, atomicWriteFile(path, []byte(out))
	}
	return false, nil
}

// tomlHeaders returns the line index of every table header and whether it
// is a [[tools.command]] header. A line inside a multi-line string is never
// a header; spaces inside the brackets and a trailing comment are allowed.
func tomlHeaders(lines []string) (starts []int, tools []bool) {
	state := "" // "" outside, `'''` or `"""` inside a multi-line string
	for i, l := range lines {
		if state == "" {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "[") {
				starts = append(starts, i)
				name := strings.ReplaceAll(strings.TrimSpace(stripTOMLComment(t)), " ", "")
				tools = append(tools, strings.ReplaceAll(name, "\t", "") == "[[tools.command]]")
			}
		}
		state = scanTOMLLine(l, state)
	}
	return starts, tools
}

// scanTOMLLine returns the multi-line-string state after line l, starting
// in state. It skips single-line strings and comments so a quote or a "#"
// inside them cannot flip the state.
func scanTOMLLine(l, state string) string {
	for i := 0; i < len(l); {
		switch state {
		case `'''`:
			j := strings.Index(l[i:], `'''`)
			if j < 0 {
				return state
			}
			i += j + 3
			for i < len(l) && l[i] == '\'' { // up to two quotes may end the content
				i++
			}
			state = ""
		case `"""`:
			j := indexUnescaped(l[i:], `"""`)
			if j < 0 {
				return state
			}
			i += j + 3
			for i < len(l) && l[i] == '"' {
				i++
			}
			state = ""
		default:
			switch {
			case strings.HasPrefix(l[i:], `'''`):
				state, i = `'''`, i+3
			case strings.HasPrefix(l[i:], `"""`):
				state, i = `"""`, i+3
			case l[i] == '#':
				return ""
			case l[i] == '\'':
				j := strings.IndexByte(l[i+1:], '\'')
				if j < 0 {
					return ""
				}
				i += j + 2
			case l[i] == '"':
				j := indexUnescaped(l[i+1:], `"`)
				if j < 0 {
					return ""
				}
				i += j + 2
			default:
				i++
			}
		}
	}
	return state
}

// indexUnescaped is strings.Index for a basic-string delimiter that is not
// preceded by an odd run of backslashes.
func indexUnescaped(s, sub string) int {
	for from := 0; ; {
		j := strings.Index(s[from:], sub)
		if j < 0 {
			return -1
		}
		k, bs := from+j, 0
		for p := k - 1; p >= 0 && s[p] == '\\'; p-- {
			bs++
		}
		if bs%2 == 0 {
			return k
		}
		from = k + 1
	}
}

// stripTOMLComment drops a trailing comment from a header line (headers
// hold no quoted text gg writes; a quoted key is left as is).
func stripTOMLComment(t string) string {
	if i := strings.IndexByte(t, '#'); i >= 0 && !strings.ContainsAny(t[:i], `"'`) {
		return t[:i]
	}
	return t
}

// sameOutsideToolBlock is the write's safety net: before and after must
// decode to the same document except the index-th tools.command entry.
func sameOutsideToolBlock(before, after []byte, index int) error {
	var a, b map[string]any
	if err := toml.Unmarshal(before, &a); err != nil {
		return fmt.Errorf("config: refusing to rewrite: %w", err)
	}
	if err := toml.Unmarshal(after, &b); err != nil {
		return fmt.Errorf("config: refusing to rewrite: the result would not parse: %w", err)
	}
	ca, cb := toolCommandList(a), toolCommandList(b)
	if len(ca) != len(cb) || index >= len(ca) {
		return fmt.Errorf("config: refusing to rewrite: the tool block count would change")
	}
	for i := range ca {
		if i != index && !reflect.DeepEqual(ca[i], cb[i]) {
			return fmt.Errorf("config: refusing to rewrite: another tool block would change")
		}
	}
	dropToolCommands(a)
	dropToolCommands(b)
	if !reflect.DeepEqual(a, b) {
		return fmt.Errorf("config: refusing to rewrite: settings outside the tool block would change")
	}
	return nil
}

func toolCommandList(doc map[string]any) []any {
	tools, _ := doc["tools"].(map[string]any)
	list, _ := tools["command"].([]any)
	return list
}

func dropToolCommands(doc map[string]any) {
	if tools, ok := doc["tools"].(map[string]any); ok {
		delete(tools, "command")
		if len(tools) == 0 {
			delete(doc, "tools")
		}
	}
}

// ToolBlockLine is the 1-based line of the effective (last) [[tools.command]]
// header whose (category, name) is key in the file at path; 0 when there is
// none or the file cannot be read. The review's "edit" opens the editor here.
func ToolBlockLine(path, key string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	lines := strings.SplitAfter(string(raw), "\n")
	starts, tools := tomlHeaders(lines)
	for si := len(starts) - 1; si >= 0; si-- {
		if !tools[si] {
			continue
		}
		end := len(lines)
		if si+1 < len(starts) {
			end = starts[si+1]
		}
		var one struct {
			Tools ToolsConfig `toml:"tools"`
		}
		if toml.Unmarshal([]byte(strings.Join(lines[starts[si]:end], "")), &one) == nil &&
			len(one.Tools.Command) == 1 && one.Tools.Command[0].Key() == key {
			return starts[si] + 1
		}
	}
	return 0
}
