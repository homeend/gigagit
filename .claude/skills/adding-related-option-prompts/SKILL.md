---
name: adding-related-option-prompts
description: Use when a gigagit Settings toggle should offer a one-question follow-up about a RELATED option (e.g. Show graph off → offer Commit sort plain), or when changing the related-prompt registry, popup, or promptstate suppression store.
---

# Adding a related-option prompt

## What this is

When a Settings toggle lands on a value that makes a *different* option worth
reconsidering, gg asks ONE follow-up question — options are always
`<Yes, do it>` / `Not now` / `No — don't ask again`. The machinery is a
data-driven registry (`internal/tui/related_prompts.go`), a generic popup
(`internal/tui/related_prompt_popup.go`), and a machine-global suppression
store (`internal/promptstate`, one TOML file at `<state>/gg/prompts.toml`).
Adding a prompt = adding ONE registry entry + one hook line; the popup and
store are already generic.

## Checklist for a new prompt

1. **Registry entry** in `relatedPrompts` (`internal/tui/related_prompts.go`):

   ```go
   {
       id:       "<setting>_<newvalue>.<related>_<target>", // e.g. show_graph_off.commit_sort_plain
       setting:  setting<X>,                                 // const naming the triggering toggle
       question: "…one sentence, states the WHY…?",
       yesLabel: "Yes, <verb> <target>",
       when: func(m Model, newValue string) bool {
           return newValue == "<trigger-value>" && /* related option's CURRENT state makes it worthwhile */
       },
       apply: func(m Model) (Model, tea.Cmd) { return m.<theRelatedSettingsRowFunc>() },
   }
   ```

   - **id is forever.** It is the persisted suppression key — never rename one
     that shipped (a rename un-suppresses the prompt for everyone).
   - **`when` must check the live config**, not just newValue: an
     already-aligned config must ask nothing. Mind unset-vs-default resolution
     (use the resolving helper, e.g. `m.commitSort()`, never the raw cfg field).
   - **`apply` must reuse the related Settings row's exact code path**
     (toggle/cycle func) — persist + side effects (feed re-walks etc.) come
     free and stay consistent. Never write a parallel setter. If the row func
     is a cycle over two modes and `when` pins the current one, one cycle IS
     the targeted set.

2. **Hook the triggering toggle** (if it isn't hooked yet). In the Settings
   enter handler (`settings_popup.go`), after the toggle applies:

   ```go
   m = m.toggle<X>()
   return m.maybeRelatedPrompt(setting<X>, m.cfg.<Section>.<Field>)
   ```

   Pass the setting's FRESH value (after the toggle mutated cfg). One
   follow-up max — `relatedPromptFor` returns the first match only.

3. **Suppression lifecycle** — nothing to do. `relatedPromptFor` filters
   suppressed ids; "No — don't ask again" persists via
   `promptstate.Store.SuppressPrompt`. Suppression is GLOBAL (all repos) by
   design. "Not now" is session-only (no record anywhere).

## Rules

- **Never trap:** esc = Not now. The popup swallows every key.
- Prompts are UX memory, NOT config: no `.gg.toml` key, no `settingDocs`
  entry, no `internal/config` writer.
- The popup footer names `prompts.toml` — keep it that way (discoverable,
  resettable by deleting the file or the id line).
- `internal/promptstate` is TUI-owned (archtest DAG leaf); do NOT move it
  behind `domain` and do NOT let it import anything above itself.

## Tests to write (see `related_prompt_popup_test.go` for exemplars)

- Trigger unit tests on `relatedPromptFor`: fires on the trigger value +
  precondition; silent when the related option is already aligned; silent
  when suppressed; nil store still prompts.
- Popup wiring through the REAL key path (open Settings, select row, enter):
  prompt pushes; Yes applies via the row's code path AND persists its config;
  esc returns to Settings and changes nothing; don't-ask-again writes the
  store and the prompt never fires again; global keys are swallowed.
- Always inject a temp-file store (`m.promptStore =
  promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))`) —
  a test must never touch the developer's real prompts.toml.
