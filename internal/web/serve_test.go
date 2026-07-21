package web

import (
	"strings"
	"testing"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		ok   bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0", false},
		{"", false},
		{"192.168.1.5", false},
		{"example.com", false},
	}
	for _, c := range cases {
		if got := isLoopbackHost(c.host); got != c.ok {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", c.host, got, c.ok)
		}
	}
}

func TestListenRefusesPublicAddr(t *testing.T) {
	if _, _, err := listen("0.0.0.0:0"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v, want loopback refusal", err)
	}
	if _, _, err := listen(":8080"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v, want loopback refusal for empty host", err)
	}
}

func TestListenDefaultLoopback(t *testing.T) {
	ln, url, err := listen("")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("url = %q, want http://127.0.0.1:<port>", url)
	}
}
