package template

import (
	"reflect"
	"testing"
)

func TestUserLabels(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{"none", "b/from-<parent-branch>", nil},
		{"one", "issue/<user:issue-id>", []string{"issue-id"}},
		{"distinct in order", "<user:user>/fix/<user:issue-id>", []string{"user", "issue-id"}},
		{"dedup repeated", "<user:id>-<user:id>", []string{"id"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UserLabels(tc.tmpl)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("UserLabels(%q) = %v, want %v", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestSeqNames(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{"none", "b/<parent-branch>", nil},
		{"padded", "deploy-<seq:deploy:4>", []string{"deploy"}},
		{"unpadded", "n<seq:issue>", []string{"issue"}},
		{"distinct in order", "<seq:b>-<seq:a>-<seq:b>", []string{"b", "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SeqNames(tc.tmpl)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SeqNames(%q) = %v, want %v", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestGoLayout(t *testing.T) {
	tests := []struct{ in, want string }{
		{"yyyy-MM-dd", "2006-01-02"},
		{"yyyy/MM/dd HH:mm:ss", "2006/01/02 15:04:05"},
		{"HH:mm", "15:04"},
		{"yyyyMMdd", "20060102"},
	}
	for _, tc := range tests {
		if got := goLayout(tc.in); got != tc.want {
			t.Errorf("goLayout(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
