package cli

import (
	"reflect"
	"testing"
)

func TestTokenizeBatchLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"simple words", "commit -m msg", []string{"commit", "-m", "msg"}},
		{"double quoted spaces", `commit -m "two words"`, []string{"commit", "-m", "two words"}},
		{"single quoted", `commit -m 'a "b" c'`, []string{"commit", "-m", `a "b" c`}},
		{"adjacent quote joins token", `-m"x y"z`, []string{"-mx yz"}},
		{"tabs separate", "log\t-n\t3", []string{"log", "-n", "3"}},
		{"empty", "", nil},
		{"whitespace only", "   \t ", nil},
		{"quoted empty token survives", `commit -m ""`, []string{"commit", "-m", ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := tokenizeBatchLine(c.in)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestTokenizeBatchLineUnterminated(t *testing.T) {
	for _, in := range []string{`commit -m "oops`, `commit -m 'oops`} {
		if _, err := tokenizeBatchLine(in); err == nil {
			t.Fatalf("want unterminated-quote error for %q", in)
		}
	}
}
