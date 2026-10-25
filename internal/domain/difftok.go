package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/syntax"
)

// contentSum names one side's bytes for the token store and the frontends'
// "do these runs belong to the diff on screen" check: lowercase hex SHA-256,
// "" for an absent side (nil), which never has runs. An EMPTY side is
// present and gets the sum of nothing.
func contentSum(b []byte) string {
	if b == nil {
		return ""
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// tokEntry is the token cache's value: one side's per-line runs.
type tokEntry struct{ toks [][]syntax.Tok }

// Size implements cache.Sized (24 = a slice header; syntax.Tok is two ints
// and a byte, padded to 24).
func (e tokEntry) Size() int {
	n := 0
	for _, line := range e.toks {
		n += 24 + 24*len(line)
	}
	return n
}

// lexJob is one running lex that any number of callers wait on. It runs on
// its own context, cancelled when the last waiter leaves.
type lexJob struct {
	waiters int
	cancel  context.CancelFunc
	done    chan struct{}
	toks    [][]syntax.Tok
	err     error
}

// tokStore holds syntax runs keyed by CONTENT (language + contentSum), not
// by diff: revision N's new side is revision N+1's old side, so a walk
// through a file's history lexes one side per step. A lex in progress is
// shared by every caller asking for the same key (the cache package does
// not coalesce misses), and stops when none of them wants it any more; a
// stopped lex stores nothing.
type tokStore struct {
	cache cache.Cache // nil = share running lexes, cache nothing
	lex   func(ctx context.Context, lang string, src []byte) ([][]syntax.Tok, error)

	mu      sync.Mutex
	running map[string]*lexJob
}

func newTokStore(c cache.Cache) *tokStore {
	return &tokStore{cache: c, lex: syntax.LexCtx, running: map[string]*lexJob{}}
}

func tokKey(lang, sum string) string { return lang + ":" + sum }

// Get answers a cached side's runs without lexing.
func (s *tokStore) Get(lang, sum string) ([][]syntax.Tok, bool) {
	if s.cache == nil {
		return nil, false
	}
	v, ok := s.cache.Get(tokKey(lang, sum))
	if !ok {
		return nil, false
	}
	return v.(tokEntry).toks, true
}

// Lex answers src's runs: from the cache, by joining the lex already running
// for the same content, or by starting one. ctx bounds only THIS caller's
// wait; the lex itself stops when its last waiter has gone.
func (s *tokStore) Lex(ctx context.Context, lang, sum string, src []byte) ([][]syntax.Tok, error) {
	if toks, ok := s.Get(lang, sum); ok {
		return toks, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := tokKey(lang, sum)
	s.mu.Lock()
	job := s.running[key]
	if job == nil {
		jctx, cancel := context.WithCancel(context.Background())
		job = &lexJob{cancel: cancel, done: make(chan struct{})}
		s.running[key] = job
		go s.run(jctx, key, job, lang, src)
	}
	job.waiters++
	s.mu.Unlock()

	select {
	case <-job.done:
		s.leave(key, job)
		return job.toks, job.err
	case <-ctx.Done():
		s.leave(key, job)
		return nil, ctx.Err()
	}
}

func (s *tokStore) run(ctx context.Context, key string, job *lexJob, lang string, src []byte) {
	toks, err := s.lex(ctx, lang, src)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err() // a lex that ignored its context must not be cached as finished
	}
	if err == nil && s.cache != nil {
		_, _ = s.cache.GetOrLoad(key, func() (any, error) { return tokEntry{toks}, nil })
	}
	s.mu.Lock()
	job.toks, job.err = toks, err
	if s.running[key] == job {
		delete(s.running, key)
	}
	s.mu.Unlock()
	job.cancel()
	close(job.done)
}

// leave drops one waiter; the last one out cancels a lex still running and
// forgets it, so a later caller starts afresh instead of joining a dying job.
func (s *tokStore) leave(key string, job *lexJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.waiters--
	if job.waiters > 0 {
		return
	}
	select {
	case <-job.done:
	default:
		job.cancel()
		if s.running[key] == job {
			delete(s.running, key)
		}
	}
}

// waiters is how many callers wait on the running lex for lang/sum (0 when
// none runs). Tests only.
func (s *tokStore) waiters(lang, sum string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job := s.running[tokKey(lang, sum)]; job != nil {
		return job.waiters
	}
	return 0
}
