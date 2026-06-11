package observ

import "regexp"

// userinfoURL matches the "user:password@" portion of a URL, capturing the user.
var userinfoURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]+):[^@/\s]+@`)

// credentialConfig matches a "credential.*=value" git -c config string.
var credentialConfig = regexp.MustCompile(`^(credential\.[^=]*)=.*$`)

// Redact returns a copy of args with secrets masked: URL passwords and
// credential.* config values are replaced with "<redacted>".
func Redact(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		a = userinfoURL.ReplaceAllString(a, `$1:<redacted>@`)
		a = credentialConfig.ReplaceAllString(a, `$1=<redacted>`)
		out[i] = a
	}
	return out
}
