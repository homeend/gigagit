# commitfeed (CQRS stage 3) — diagrams

Companion to the stage-3 design. Renders on GitHub or any Mermaid viewer.

Decision: the `CommitFeed` is a **stateful domain read-model** in
`internal/domain` and is the **single source of truth for commits**.
`Snapshot` no longer reads commits. The TUI signals intent and subscribes;
it never accumulates.

---

## Class diagram — components & relationships

```mermaid
classDiagram
    class Model {
        <<TUI value receiver>>
        +svc *Service
        +feed *CommitFeed
        +commits []Commit
        +commitsExhausted bool
        +sel map~panel~int
        +loadCmd() Cmd
        +loadMoreCmd() Cmd
    }

    class Service {
        <<domain>>
        -repo *git.Repo
        -gate *repogate.Gate
        -flight flightGroup
        +Execute(op) Result
        +Snapshot(ctx) Snapshot
        +Status(ctx) WorkingTreeStatus
        +Worktrees(ctx) []Worktree
        +CommitFeed() *CommitFeed
        ~logPage(ctx, limit, skip) []Commit
    }

    class CommitFeed {
        <<domain read-model>>
        -svc *Service
        -mu sync.Mutex
        -commits []Commit
        -skip int
        -exhausted bool
        -gen int
        -inFlight bool
        +LoadInitial(ctx) Page
        +LoadMore(ctx) Page, bool
        +NeedsMore(sel) bool
        +Snapshot() FeedState
        +Reset()
        +Gen() int
    }

    class Snapshot {
        <<value>>
        +Status WorkingTreeStatus
        +Branches []Branch
        +Worktrees []Worktree
        +CurrentWorktree string
        +GitCommonDir string
        +HeadTimes map
    }

    class FeedState {
        <<value, returned by Snapshot()>>
        +Commits []Commit
        +Exhausted bool
        +Gen int
    }

    class Repo {
        <<internal/git>>
        +Log(ctx, limit, skip) []Commit
        +Status(ctx) WorkingTreeStatus
        +Branches(ctx) []Branch
    }

    class Gate {
        <<internal/repogate>>
        +Acquire(ctx, Read, label) Reservation
    }

    Model o-- Service : holds (pointer)
    Model o-- CommitFeed : holds (pointer)
    Service ..> Snapshot : returns
    Service ..> CommitFeed : vends via CommitFeed()
    CommitFeed --> Service : reads via logPage()
    CommitFeed ..> FeedState : returns from Snapshot()
    Service --> Gate : every query takes a Read reservation
    Service --> Repo : Snapshot fan-out + logPage call verbs
    CommitFeed *-- Commit : accumulates
    note for Snapshot "no Commits field — commits\nmoved to CommitFeed"
    note for CommitFeed "single source of truth\nfor the commit list"
```

---

## Sequence — startup (parallel Snapshot + initial commit page)

```mermaid
sequenceDiagram
    autonumber
    participant UI as TUI (loadCmd)
    participant S as Service
    participant F as CommitFeed
    participant G as repogate.Gate
    participant R as git.Repo

    Note over UI: one tea.Cmd, off the UI thread
    par Snapshot (non-commit reads)
        UI->>S: Snapshot(ctx)
        S->>G: Acquire(Read "snapshot")
        G-->>S: reservation
        S->>R: Status / Branches / Worktrees / TopLevel / CommonDir (parallel)
        R-->>S: results
        S-->>UI: Snapshot{status, branches, worktrees, etc}
    and Initial commit page (Feed owns it)
        UI->>F: LoadInitial(ctx)
        F->>S: logPage(limit=50, skip=0)
        S->>G: Acquire(Read "commits")
        G-->>S: reservation
        S->>R: Log(50, 0)
        R-->>S: 50 commits
        S-->>F: 50 commits
        Note over F: commits=50, skip=50, exhausted if short page
        F-->>UI: Page{commits, exhausted, gen}
    end
    Note over UI: join both results
    UI->>UI: assign snapshot fields, commits, and exhausted into the Model
    Note over UI: first paint — both reads hid behind git status (the long pole)
```

---

## Sequence — paging (UI signals intent, Feed loads, UI re-subscribes)

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant UI as TUI (Update)
    participant F as CommitFeed
    participant S as Service
    participant R as git.Repo

    User->>UI: press down / j / pgdn
    UI->>UI: m.sel[Commits]++ (clamped)
    UI->>F: NeedsMore(sel)?
    alt sel near end AND not exhausted AND not in-flight AND no active filter
        F-->>UI: true
        UI-->>UI: return loadMoreCmd(feed)
        Note over UI,F: off the UI thread
        UI->>F: LoadMore(ctx)
        F->>F: capture gen0, set inFlight
        F->>S: logPage(limit=200, skip=250)
        S->>R: Log(200, 250) gated plus singleflight
        R-->>S: page (up to 200)
        S-->>F: page
        alt gen unchanged (no reRoot/reload meanwhile)
            F->>F: append, hash-dedupe, advance skip, set exhausted if short page
            F-->>UI: commitsPagedMsg{gen0}
            UI->>F: Snapshot()
            F-->>UI: FeedState{commits, exhausted, gen}
            UI->>UI: m.commits = state.Commits (250 to 450), cursor untouched
            Note over UI: label "Commits 450+" then "Commits 437" when exhausted
        else gen changed (repo was reloaded mid-page)
            F-->>UI: stale page dropped
        end
    else at end OR exhausted OR in-flight OR filtering
        F-->>UI: false (no-op)
    end
```

---

## State — the Feed lifecycle

```mermaid
stateDiagram-v2
    [*] --> Empty
    Empty --> Loaded : LoadInitial (page 0, skip=50)
    Loaded --> Loaded : LoadMore (append, skip += len)
    Loaded --> Exhausted : short page (fewer than limit)
    Exhausted --> Exhausted : NeedsMore() = false
    Loaded --> Empty : Reset (reload / 'r' / post-op) gen++
    Exhausted --> Empty : Reset gen++
    note right of Loaded
        NeedsMore(sel) is true when
        not exhausted, not inFlight,
        and sel is within threshold of the end
    end note
    note right of Empty
        reRoot makes a fresh Feed.
        Reset reuses it, bumping gen
        so in-flight pages drop
    end note
```
