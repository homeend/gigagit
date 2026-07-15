package i18n

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func reset(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _ = SetLanguage("", "") })
}

// writeLang drops a custom bundle into a temp lang dir and returns the dir.
func writeLang(t *testing.T, code, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, code+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTFallsBackToKeyInEnglish(t *testing.T) {
	reset(t)
	if got := T("Compare branches"); got != "Compare branches" {
		t.Fatalf("T = %q, want the key back", got)
	}
	if got := T("committed %s %s", "abc123", "subject"); got != "committed abc123 subject" {
		t.Fatalf("T with args = %q", got)
	}
	// no args → verbatim, a stray % must not be mangled
	if got := T("100% done"); got != "100% done" {
		t.Fatalf("verbatim = %q", got)
	}
}

func TestSetLanguageCustomBundleAndArgsReorder(t *testing.T) {
	reset(t)
	dir := writeLang(t, "xx", `[meta]
name = "Xxish"

[strings]
"Compare branches" = "Xx compare"
"committed %s %s" = "xx %[2]s <= %[1]s"
`)
	if err := SetLanguage("xx", dir); err != nil {
		t.Fatal(err)
	}
	if ActiveCode() != "xx" || ActiveName() != "Xxish" {
		t.Fatalf("active = %s/%s", ActiveCode(), ActiveName())
	}
	if got := T("Compare branches"); got != "Xx compare" {
		t.Fatalf("T = %q", got)
	}
	if got := T("committed %s %s", "abc", "subj"); got != "xx subj <= abc" {
		t.Fatalf("reorder = %q", got)
	}
	if got := T("not translated"); got != "not translated" {
		t.Fatalf("miss must fall back, got %q", got)
	}
}

func TestSetLanguageUnknownCodeErrorsAndKeepsActive(t *testing.T) {
	reset(t)
	dir := writeLang(t, "xx", "[meta]\nname = \"Xxish\"\n\n[strings]\n\"a\" = \"b\"\n")
	if err := SetLanguage("xx", dir); err != nil {
		t.Fatal(err)
	}
	if err := SetLanguage("nope", dir); err == nil {
		t.Fatal("unknown code must error")
	}
	if ActiveCode() != "xx" {
		t.Fatalf("failed SetLanguage must keep the previous catalog, got %s", ActiveCode())
	}
}

func TestSetLanguageEnResets(t *testing.T) {
	reset(t)
	dir := writeLang(t, "xx", "[meta]\nname = \"Xxish\"\n\n[strings]\n\"a\" = \"b\"\n")
	if err := SetLanguage("xx", dir); err != nil {
		t.Fatal(err)
	}
	if err := SetLanguage("en", ""); err != nil {
		t.Fatal(err)
	}
	if ActiveCode() != "en" || ActiveName() != "English" || T("a") != "a" {
		t.Fatalf("en reset broken: %s/%s/%s", ActiveCode(), ActiveName(), T("a"))
	}
}

func TestVerbMismatchSkipsKeyOnly(t *testing.T) {
	reset(t)
	dir := writeLang(t, "xx", `[meta]
name = "Xxish"

[strings]
"good %s" = "xx %s"
"bad %s" = "xx no verb"
`)
	if err := SetLanguage("xx", dir); err != nil {
		t.Fatal(err)
	}
	if got := T("good %s", "v"); got != "xx v" {
		t.Fatalf("good key = %q", got)
	}
	if got := T("bad %s", "v"); got != "bad v" {
		t.Fatalf("bad key must fall back to English, got %q", got)
	}
}

func TestMergeBundleOverlaysPerKey(t *testing.T) {
	dst := map[string]string{}
	if _, err := mergeBundle(dst, []byte("[strings]\n\"a\" = \"base-a\"\n\"b\" = \"base-b\"\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeBundle(dst, []byte("[strings]\n\"b\" = \"over-b\"\n")); err != nil {
		t.Fatal(err)
	}
	if dst["a"] != "base-a" || dst["b"] != "over-b" {
		t.Fatalf("overlay = %v", dst)
	}
}

func TestMalformedBundleErrors(t *testing.T) {
	reset(t)
	dir := writeLang(t, "xx", "not toml [ at all")
	if err := SetLanguage("xx", dir); err == nil {
		t.Fatal("malformed bundle must error")
	}
}

func TestAvailableOrderingAndOverlayMerge(t *testing.T) {
	dir := t.TempDir()
	// a brand-new custom language, and a custom overlay of built-in ja renaming it
	os.WriteFile(filepath.Join(dir, "xx.toml"), []byte("[meta]\nname = \"Xxish\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ja.toml"), []byte("[meta]\nname = \"custom-ja\"\n"), 0o644)
	langs := Available(dir)
	if langs[0].Code != "en" || langs[0].Name != "English" {
		t.Fatalf("first = %+v, want English", langs[0])
	}
	codes := map[string]string{}
	for _, l := range langs {
		if _, dup := codes[l.Code]; dup {
			t.Fatalf("duplicate code %s", l.Code)
		}
		codes[l.Code] = l.Name
	}
	for _, want := range []string{"en", "ja", "ko", "zh", "ru", "xx"} {
		if _, ok := codes[want]; !ok {
			t.Fatalf("missing %s in %v", want, langs)
		}
	}
	if codes["ja"] != "custom-ja" {
		t.Fatalf("custom overlay name must win, got %q", codes["ja"])
	}
	if codes["xx"] != "Xxish" {
		t.Fatalf("custom name = %q", codes["xx"])
	}
	// last entries are the custom-only codes
	if langs[len(langs)-1].Code != "xx" {
		t.Fatalf("custom codes must come last: %v", langs)
	}
}

func TestBuiltinsExposesAllFour(t *testing.T) {
	b := Builtins()
	for _, code := range []string{"ja", "ko", "zh", "ru"} {
		if _, ok := b[code]; !ok {
			t.Fatalf("missing built-in %s", code)
		}
	}
}

func TestConcurrentTAndSetLanguage(t *testing.T) {
	reset(t)
	dir := writeLang(t, "xx", "[meta]\nname = \"Xxish\"\n\n[strings]\n\"k\" = \"v\"\n")
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = T("k")
			}
		}()
	}
	for range 50 {
		if err := SetLanguage("xx", dir); err != nil {
			t.Error(err)
		}
		_ = SetLanguage("", "")
	}
	wg.Wait()
}

func TestSetLanguageSurfacesUnreadableCustomFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 not enforced")
	}
	if os.Getuid() == 0 {
		t.Skip("root ignores file modes")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "ja.toml")
	if err := os.WriteFile(p, []byte("[meta]\nname=\"x\"\n[strings]\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer SetLanguage("", "") // restore English for other tests
	if err := SetLanguage("ja", dir); err == nil {
		t.Fatal("want error: custom file exists but is unreadable (EACCES)")
	}
	// A merely-absent custom file stays fine: the embedded bundle carries it.
	if err := SetLanguage("ja", t.TempDir()); err != nil {
		t.Fatalf("absent custom file must not error: %v", err)
	}
}

func TestActiveTranslationsEmptyForEnglish(t *testing.T) {
	reset(t)
	if m := ActiveTranslations(); len(m) != 0 {
		t.Fatalf("English ActiveTranslations() = %v, want empty", m)
	}
}

func TestActiveTranslationsNonEmptyAfterSetLanguage(t *testing.T) {
	reset(t)
	if err := SetLanguage("ja", ""); err != nil {
		t.Fatal(err)
	}
	m := ActiveTranslations()
	if len(m) == 0 {
		t.Fatal("ActiveTranslations() empty after SetLanguage(ja)")
	}
	if _, ok := m["error: %s"]; !ok {
		t.Fatalf("ActiveTranslations() missing known key %q; got %d keys", "error: %s", len(m))
	}
}
