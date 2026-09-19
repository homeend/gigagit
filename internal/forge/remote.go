package forge

import (
	"net/url"
	"strings"
)

// RepoSlug reduces a git remote URL to "host/owner/name" (lower-cased, no
// .git, no credentials, no port) so an ssh and an https spelling of one
// repository compare equal. "" means the URL names no forge repository.
func RepoSlug(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else if at := strings.Index(remote, "@"); at >= 0 && strings.Contains(remote[at:], ":") {
		// scp-like: user@host:owner/name
		rest := remote[at+1:]
		colon := strings.Index(rest, ":")
		host, path = rest[:colon], rest[colon+1:]
	} else {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if host == "" || len(parts) < 2 {
		return ""
	}
	return strings.ToLower(host + "/" + strings.Join(parts, "/"))
}
