package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestCmdCheckoutParseErrors covers cmdCheckout's flag-parsing error paths.
// All of these return before the service is ever touched, so a nil
// *domain.Service is fine here.
func TestCmdCheckoutParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "--as missing value",
			args:    []string{"origin/foo", "--as"},
			wantErr: "checkout: --as requires a branch name",
		},
		{
			name:    "--as= empty value",
			args:    []string{"origin/foo", "--as="},
			wantErr: "checkout: --as requires a branch name",
		},
		{
			name:    "unknown flag",
			args:    []string{"origin/foo", "--bogus"},
			wantErr: "checkout: unknown flag",
		},
		{
			name:    "two positional refs",
			args:    []string{"origin/foo", "origin/bar"},
			wantErr: "checkout: too many arguments",
		},
		{
			name:    "missing ref entirely",
			args:    []string{},
			wantErr: "checkout: a remote branch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cmdCheckout(nil, tt.args, &stdout, &stderr)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (stderr: %s)", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf("stderr = %q, want to contain %q", stderr.String(), tt.wantErr)
			}
		})
	}
}
