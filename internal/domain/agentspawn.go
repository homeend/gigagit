package domain

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
)

// AgentKickoff is the one line a spawned worker's <prompt> slot carries: the
// brief itself never rides a command line (spec §2 ruling 5).
const AgentKickoff = "You were started by gg as a worker agent. Read your task with the gg tool agent_task - or gg agent task in a shell - then do it following the Worker protocol section of the gg-delegate skill - gg skill path gg-delegate prints where it is."

// SpawnRecord is what this process knows about an agent session it started.
type SpawnRecord struct {
	Parent   string // full session id of the agent that spawned it ("" = the user)
	Brief    string // the free-form task (agent_task)
	Worktree string
	Spawned  bool   // started by an agent (counts against max_spawned; may not spawn)
	Name     string // the user's name for the session ("" = unnamed); cleaned by StartAgentSession
}

// spawnRegistry is process-global, like Sessions(): it survives reRoot.
type spawnRegistry struct {
	mu      sync.Mutex
	tokens  map[string]string // token -> full session id
	records map[string]SpawnRecord
	pending int // reserved slots whose start has not finished
	// Stage 3b: what workers reported and what each caller's wait already
	// delivered. Pruned with the records (a removed session forgets both).
	reports   map[string][]AgentReport           // full id -> oldest first, ≤ maxReportsKept
	reportSeq uint64                             // process-global, monotonic
	marks     map[string]map[string]deliveryMark // caller -> worker -> delivered
	noticed   map[string]time.Time               // full id -> when its last report notice went out
	bc        agentsession.Broadcaster           // wakes waiters on a report
}

var (
	spawnMu  sync.Mutex
	spawnReg = newSpawnRegistry()
)

func newSpawnRegistry() *spawnRegistry {
	return &spawnRegistry{tokens: map[string]string{}, records: map[string]SpawnRecord{},
		reports: map[string][]AgentReport{}, marks: map[string]map[string]deliveryMark{},
		noticed: map[string]time.Time{}}
}

func registry() *spawnRegistry { spawnMu.Lock(); defer spawnMu.Unlock(); return spawnReg }

// useSpawnRegistry installs a fresh registry (tests) and returns the restore.
func useSpawnRegistry() func() {
	spawnMu.Lock()
	prev := spawnReg
	spawnReg = newSpawnRegistry()
	spawnMu.Unlock()
	return func() { spawnMu.Lock(); spawnReg = prev; spawnMu.Unlock() }
}

// FullSessionID is a session's GG_SESSION_ID: <ProcTag>/<id>.
func FullSessionID(id SessionID) string { return agentsession.ProcTag() + "/" + string(id) }

// localID maps a full id back to this process's session id ("" = not ours).
func localID(full string) SessionID {
	id, ok := strings.CutPrefix(full, agentsession.ProcTag()+"/")
	if !ok || id == "" {
		return ""
	}
	return SessionID(id)
}

// runningLocal reports a full id naming a RUNNING session of this process.
func runningLocal(full string) (*AgentSession, bool) {
	id := localID(full)
	if id == "" {
		return nil, false
	}
	s, ok := Sessions().Get(id)
	if !ok || s.Info().State != SessionRunning {
		return nil, false
	}
	return s, true
}

// prune drops records and tokens of sessions no longer listed (removed);
// called under r.mu on every read — the Manager has no removal hook.
func (r *spawnRegistry) prune() {
	listed := map[string]bool{}
	for _, in := range Sessions().List() {
		listed[FullSessionID(in.ID)] = true
	}
	for id := range r.records {
		if !listed[id] {
			delete(r.records, id)
		}
	}
	for tok, id := range r.tokens {
		if !listed[id] {
			delete(r.tokens, tok)
		}
	}
	for id := range r.reports {
		if !listed[id] {
			delete(r.reports, id)
			delete(r.noticed, id)
		}
	}
	for caller, byWorker := range r.marks {
		if !listed[caller] {
			delete(r.marks, caller)
			continue
		}
		for id := range byWorker {
			if !listed[id] {
				delete(byWorker, id)
			}
		}
	}
}

func mintToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand: " + err.Error()) // never on a supported OS
	}
	return hex.EncodeToString(b[:])
}

func bindRecord(full string, rec SpawnRecord) {
	r := registry()
	r.mu.Lock()
	r.records[full] = rec
	r.mu.Unlock()
}

func bindToken(tok, full string) {
	r := registry()
	r.mu.Lock()
	r.tokens[tok] = full
	r.mu.Unlock()
}

// VerifyAgentToken maps a bearer token to the full id of the RUNNING agent
// session it was minted for; false for an unknown token or a session that
// exited (a killed worker's token stops working at once).
func VerifyAgentToken(tok string) (string, bool) {
	r := registry()
	r.mu.Lock()
	r.prune()
	full, ok := r.tokens[tok]
	r.mu.Unlock()
	if !ok {
		return "", false
	}
	if _, live := runningLocal(full); !live {
		return "", false
	}
	return full, true
}

// AgentRecord is the spawn record of a session of this process.
func AgentRecord(full string) (SpawnRecord, bool) {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	rec, ok := r.records[full]
	return rec, ok
}

// AgentDescends reports target being caller's child, grandchild, …
func AgentDescends(target, caller string) bool {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	for cur := target; !seen[cur]; {
		seen[cur] = true
		rec, ok := r.records[cur]
		if !ok || rec.Parent == "" {
			return false
		}
		if rec.Parent == caller {
			return true
		}
		cur = rec.Parent
	}
	return false
}

// reserveSpawnSlot takes one of limit slots (live spawned + pending). The
// returned release(started) frees the pending slot; a started session then
// counts through its record instead.
func reserveSpawnSlot(limit int) (func(started bool), bool) {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	live := 0
	for id, rec := range r.records {
		if _, ok := runningLocal(id); ok && rec.Spawned {
			live++
		}
	}
	if live+r.pending >= limit {
		return nil, false
	}
	r.pending++
	var once sync.Once
	return func(bool) { once.Do(func() { r.mu.Lock(); r.pending--; r.mu.Unlock() }) }, true
}
