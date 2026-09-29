package main

import (
	"reflect"
	"testing"
)

func TestExtractWebFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantWeb  bool
		wantAddr string
		wantRest []string
	}{
		{"absent", []string{"status"}, false, "", []string{"status"}},
		{"web", []string{"--web"}, true, "", []string{}},
		{"addr space form", []string{"--web-addr", "127.0.0.1:7777"}, true, "127.0.0.1:7777", []string{}},
		{"addr equals form", []string{"--web-addr=127.0.0.1:7777", "--record", "x"}, true, "127.0.0.1:7777", []string{"--record", "x"}},
		{"both", []string{"--web", "--web-addr", "127.0.0.1:1"}, true, "127.0.0.1:1", []string{}},
		{"no value is dropped safely", []string{"--web-addr"}, false, "", []string{}},
		{"gg web keeps its own flags", []string{"web", "--addr", "127.0.0.1:2"}, false, "", []string{"web", "--addr", "127.0.0.1:2"}},
	}
	for _, tt := range tests {
		web, addr, rest := extractWebFlags(tt.args)
		if web != tt.wantWeb || addr != tt.wantAddr || !reflect.DeepEqual(rest, tt.wantRest) {
			t.Errorf("%s: got (%v,%q,%v) want (%v,%q,%v)", tt.name, web, addr, rest, tt.wantWeb, tt.wantAddr, tt.wantRest)
		}
	}
}
