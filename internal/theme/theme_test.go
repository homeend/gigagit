package theme

import (
	"regexp"
	"strconv"
	"testing"
)

var hexOrIndex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func validColour(s string) bool {
	if hexOrIndex.MatchString(s) {
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255
}

func TestLookup(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "terminal"} {
		th, ok := Lookup(name)
		if !ok || th.Name != NameTerminal || th.Bg != "" || th.Fg != "" {
			t.Fatalf("Lookup(%q) = %+v, %v; want Terminal", name, th, ok)
		}
	}
	if th, ok := Lookup("dark"); !ok || th.Name != NameDark || th.Bg != "#0C0C0C" {
		t.Fatalf("Lookup(dark) = %+v, %v", th, ok)
	}
	if th, ok := Lookup("light"); !ok || th.Name != NameLight || th.Bg != "#F3EAD3" {
		t.Fatalf("Lookup(light) = %+v, %v", th, ok)
	}
	if _, ok := Lookup("solarized"); ok {
		t.Fatal("unknown theme must not resolve")
	}
	if _, ok := Lookup("Dark"); ok {
		t.Fatal("lookup is case-sensitive (config values are English protocol strings)")
	}
}

func TestNamesOrder(t *testing.T) {
	t.Parallel()
	got := Names()
	want := []string{"terminal", "dark", "light"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Every non-empty role must be a #rrggbb hex or a 0–255 index, and the two
// built-in colour themes must set every role except the two deliberately
// uncoloured syntax classes (Plain=0, Name=4).
func TestBuiltinsComplete(t *testing.T) {
	t.Parallel()
	for _, th := range []Theme{Dark, Light} {
		for i, c := range th.roles() {
			if c == "" {
				t.Errorf("%s: role %d is empty", th.Name, i)
			} else if !validColour(c) {
				t.Errorf("%s: role %d = %q is not a hex colour or 0–255 index", th.Name, i, c)
			}
		}
		for i, c := range th.Lanes {
			if !validColour(c) {
				t.Errorf("%s: Lanes[%d] = %q invalid", th.Name, i, c)
			}
		}
		for i, c := range th.Syntax {
			switch i {
			case 0, 4:
				if c != "" {
					t.Errorf("%s: Syntax[%d] must stay uncoloured, got %q", th.Name, i, c)
				}
			default:
				if !validColour(c) {
					t.Errorf("%s: Syntax[%d] = %q invalid", th.Name, i, c)
				}
			}
		}
	}
	var zero Theme
	for i, c := range zero.roles() {
		if c != "" {
			t.Fatalf("Terminal role %d must be empty (inherit), got %q", i, c)
		}
	}
}
