package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/repos"
)

// relatedPrompt is one entry of the related-option registry: when a Settings
// toggle lands on a value that makes a RELATED option worth reconsidering, gg
// asks one follow-up question. Options are always Yes / Not now / No — don't
// ask again; "Yes" must reuse the related Settings row's exact code path.
type relatedPrompt struct {
	id       string // stable suppression key (persisted in prompts.toml)
	setting  string // which Settings toggle triggers evaluation
	question string
	yesLabel string
	// when gates the prompt on the LIVE config after the toggle applied:
	// newValue is the setting's fresh value; check the related option's
	// current state so an already-aligned config asks nothing.
	when func(m Model, newValue string) bool
	// apply runs the "Yes" action. It must be the same code path as the
	// related option's own Settings row — never a parallel setter.
	apply func(m Model) (Model, tea.Cmd)
}

// setting ids consulted by relatedPromptFor (one per registered trigger).
const settingShowGraph = "show_graph"

// relatedPrompts is the registry. Both show_graph entries reuse
// cycleCommitSort() for "Yes": commit_sort has exactly two modes and each
// entry's `when` pins the current one, so one cycle lands on the target mode
// via the Settings row's own code path (persist + feed re-walk included).
var relatedPrompts = []relatedPrompt{
	{
		id:       "show_graph_off.commit_sort_plain",
		setting:  settingShowGraph,
		question: "Ordering only matters for graph lanes — also switch Commit sort to plain (much faster on big repos)?",
		yesLabel: "Yes, set plain",
		when: func(m Model, newValue string) bool {
			return newValue == "off" && m.commitSort() == "date-order"
		},
		apply: func(m Model) (Model, tea.Cmd) { return m.cycleCommitSort() },
	},
	{
		id:       "show_graph_on.commit_sort_dateorder",
		setting:  settingShowGraph,
		question: "The graph draws correct lanes only with date-order — switch Commit sort back to date-order?",
		yesLabel: "Yes, set date-order",
		when: func(m Model, newValue string) bool {
			return newValue == "on" && m.commitSort() == "plain"
		},
		apply: func(m Model) (Model, tea.Cmd) { return m.cycleCommitSort() },
	},
}

// relatedPromptFor returns the first registered, non-suppressed prompt whose
// trigger matches this setting change, or nil when nothing should fire. A nil
// prompt store only disables suppression persistence, never the prompts.
func (m Model) relatedPromptFor(setting, newValue string) *relatedPrompt {
	var suppressed map[string]bool
	if m.promptStore != nil {
		suppressed = m.promptStore.SuppressedPrompts()
	}
	for i := range relatedPrompts {
		rp := &relatedPrompts[i]
		if rp.setting != setting || suppressed[rp.id] || !rp.when(m, newValue) {
			continue
		}
		return rp
	}
	return nil
}

// defaultPromptStatePath puts prompts.toml beside operations.log in the gg
// state dir, reusing the repo registry's platform-appropriate resolution.
// "" when no home/state dir exists.
func defaultPromptStatePath() string {
	sp := repos.DefaultStatePath()
	if sp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sp), "prompts.toml")
}

// defaultPromptStore opens the machine-global prompt store, or nil when there
// is no state dir (prompts still show; "don't ask again" just can't persist).
func defaultPromptStore() promptstate.Store {
	path := defaultPromptStatePath()
	if path == "" {
		return nil
	}
	return promptstate.NewFileStore(path)
}
