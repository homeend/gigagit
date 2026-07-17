package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
)

func TestFooterRendersTranslatedLabels(t *testing.T) {
	setupCustomLang(t, "[meta]\nname = \"Xxish\"\n\n[strings]\n\"[,] settings\" = \"[,] XSETTINGS\"\n")
	if err := i18n.SetLanguage("xx", langDirFromEnv(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = i18n.SetLanguage("", "") })
	_ = newTestModel(t) // smoke-check model construction alongside the live language switch
	for _, b := range globalBindings() {
		if b.id == "settings" {
			if !strings.Contains(b.label, "XSETTINGS") {
				t.Fatalf("label = %q, want translated", b.label)
			}
			return
		}
	}
	t.Fatal("settings binding not found")
}
