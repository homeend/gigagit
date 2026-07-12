package buildinfo

import (
	"runtime/debug"
	"testing"
)

func buildInfoWith(mainVersion string, settings map[string]string) *debug.BuildInfo {
	bi := &debug.BuildInfo{}
	bi.Main.Version = mainVersion
	for k, v := range settings {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: k, Value: v})
	}
	return bi
}

func TestResolveUsesModuleVersionForGoInstallBuilds(t *testing.T) {
	// go install module@version stamps Main.Version but no vcs settings.
	bi := buildInfoWith("v0.1.16", nil)
	version, commit := resolve("dev", "none", bi)
	if version != "v0.1.16" {
		t.Errorf("version = %q, want %q", version, "v0.1.16")
	}
	if commit != "none" {
		t.Errorf("commit = %q, want %q", commit, "none")
	}
}

func TestResolveUsesVCSRevisionForSourceBuilds(t *testing.T) {
	// go build in a checkout stamps (devel) plus vcs settings.
	bi := buildInfoWith("(devel)", map[string]string{
		"vcs.revision": "1234567890abcdef1234567890abcdef12345678",
		"vcs.modified": "false",
	})
	version, commit := resolve("dev", "none", bi)
	if version != "dev" {
		t.Errorf("version = %q, want %q", version, "dev")
	}
	if commit != "1234567" {
		t.Errorf("commit = %q, want %q", commit, "1234567")
	}
}

func TestResolveMarksDirtySourceBuilds(t *testing.T) {
	bi := buildInfoWith("(devel)", map[string]string{
		"vcs.revision": "1234567890abcdef1234567890abcdef12345678",
		"vcs.modified": "true",
	})
	_, commit := resolve("dev", "none", bi)
	if want := "1234567-dirty"; commit != want {
		t.Errorf("commit = %q, want %q", commit, want)
	}
}

func TestResolveKeepsLdflagsValues(t *testing.T) {
	// build.sh -ldflags values must win over embedded build info.
	bi := buildInfoWith("v0.1.16", map[string]string{
		"vcs.revision": "1234567890abcdef1234567890abcdef12345678",
	})
	version, commit := resolve("0.2.0", "abc1234", bi)
	if version != "0.2.0" {
		t.Errorf("version = %q, want %q", version, "0.2.0")
	}
	if commit != "abc1234" {
		t.Errorf("commit = %q, want %q", commit, "abc1234")
	}
}

func TestResolveLeavesDefaultsWithoutBuildInfo(t *testing.T) {
	version, commit := resolve("dev", "none", nil)
	if version != "dev" || commit != "none" {
		t.Errorf("resolve(dev, none, nil) = %q, %q; want unchanged", version, commit)
	}
	version, commit = resolve("dev", "none", buildInfoWith("(devel)", nil))
	if version != "dev" || commit != "none" {
		t.Errorf("resolve with (devel), no vcs = %q, %q; want unchanged", version, commit)
	}
}

func TestStringIncludesVersion(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })
	Version = "9.9.9"
	if got := String(); got == "" {
		t.Fatal("String() returned empty")
	}
	if want := "9.9.9"; !contains(String(), want) {
		t.Fatalf("String() = %q, want it to contain %q", String(), want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
