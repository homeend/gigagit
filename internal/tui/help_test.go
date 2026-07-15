package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHelpOpensWithQuestionMark(t *testing.T) {
	m := Model{width: 80, height: 24}
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	if layerOf[*contentPopup](m) == nil {
		t.Fatal("? must open the help popup")
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "Help") || !strings.Contains(out, "pull") {
		t.Fatalf("help window must show bindings:\n%s", out)
	}
}

func TestHelpSearchFindsBinding(t *testing.T) {
	m := Model{width: 80, height: 24}
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("/")) // search starts on an explicit /
	m = u.(Model)
	for _, r := range "stash" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "stash") {
		t.Fatalf("searching 'stash' must keep the stash row:\n%s", out)
	}
	if strings.Contains(out, "reload") {
		t.Fatalf("non-matching rows must be filtered out:\n%s", out)
	}
}

// TestHelpFooterCoverage is the drift guard: every key in the footer binding
// registry (footer.go) must appear as the key column of some help row. The
// key column is the row's first whitespace-delimited field; alternates are
// /-separated (e.g. "q/ctrl+c").
func TestHelpFooterCoverage(t *testing.T) {
	var keys []string
	for _, b := range contextBindings() {
		keys = append(keys, b.key)
	}
	for _, b := range globalBindings() {
		keys = append(keys, b.key)
	}
	if len(keys) < 10 {
		t.Fatalf("binding registry looks broken, got keys %v", keys)
	}
	lines := helpContent()
	for _, k := range keys {
		found := false
		for _, l := range lines {
			if l.heading {
				continue
			}
			f := strings.Fields(l.text)
			if len(f) == 0 {
				continue
			}
			if f[0] == k {
				found = true
				continue
			}
			for _, alt := range strings.Split(f[0], "/") {
				if alt == k {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("footer key %q has no help row (helpContent key column)", k)
		}
	}
}

func TestHelpNotOpenedWhileAnotherPopupIsOpen(t *testing.T) {
	m := Model{width: 80, height: 24}
	m = m.pushLayer(&repoPopup{})
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	if layerOf[*contentPopup](m) != nil {
		t.Fatal("? must be swallowed by the open popup")
	}
}

func TestHelpDocumentsTabSwitch(t *testing.T) {
	var b strings.Builder
	for _, l := range helpContent() {
		b.WriteString(l.text)
		b.WriteString("\n")
	}
	if !strings.Contains(b.String(), "cycle the focused tab slot") {
		t.Error("help does not document the ctrl+arrow tab switch")
	}
}

func TestHelpDocumentsTags(t *testing.T) {
	var b strings.Builder
	for _, l := range helpContent() {
		b.WriteString(l.text + "\n")
	}
	if !strings.Contains(b.String(), "Tags panel") {
		t.Error("help does not document the Tags panel")
	}
}

func TestHelpDocumentsFilesStaged(t *testing.T) {
	var b strings.Builder
	for _, l := range helpContent() {
		b.WriteString(l.text + "\n")
	}
	h := b.String()
	if !strings.Contains(h, "Files panel") || !strings.Contains(h, "Staged panel") {
		t.Error("help does not document the Files and Staged panels")
	}
}

// TestHelpListsHiddenFooterKeysAtNarrowWidth: at a width where the footer
// overflows, ? must open help with a leading "More keys" section listing
// exactly the bindings fitFooter dropped, in footer order.
func TestHelpListsHiddenFooterKeysAtNarrowWidth(t *testing.T) {
	m := footerModel()
	m.width = 40
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	p := layerOf[*contentPopup](m)
	if p == nil {
		t.Fatal("? must open the help popup")
	}
	_, hidden := fitFooter(m, m.layout().w)
	if len(hidden) == 0 {
		t.Fatal("fixture must overflow at width 40")
	}
	if !p.lines[0].heading || !strings.Contains(p.lines[0].text, "More keys") {
		t.Fatalf("first help line must be the hidden-keys heading, got %+v", p.lines[0])
	}
	for i, b := range hidden {
		if !strings.Contains(p.lines[1+i].text, b.label) {
			t.Errorf("help row %d must carry %q, got %q", i, b.label, p.lines[1+i].text)
		}
	}
	// The static table must follow, un-mangled.
	if !strings.Contains(p.lines[1+len(hidden)].text, "Global") {
		t.Errorf("static help must follow the hidden section, got %q", p.lines[1+len(hidden)].text)
	}
}

func TestHelpNoHiddenSectionAtWideWidth(t *testing.T) {
	m := footerModel()
	m.width = 500
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	p := layerOf[*contentPopup](m)
	if p == nil {
		t.Fatal("? must open the help popup")
	}
	if strings.Contains(p.lines[0].text, "More keys") {
		t.Fatal("no hidden-keys section expected at a width where everything fits")
	}
}
