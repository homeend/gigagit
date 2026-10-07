package domain

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/filelock"
)

// The pending-send queue (spec 2026-10-07 §3.7): an agent's send never
// posts; it waits here, one file per repository, until the user approves,
// rejects or the agent cancels it. Several gg processes share the file
// under its lock.

// PendingSend is one queued send.
type PendingSend struct {
	ID        string        `toml:"id" json:"id"`
	Requester string        `toml:"requester" json:"requester"`
	Request   PRSendRequest `toml:"request" json:"request"`
	Created   time.Time     `toml:"created" json:"created"`
	State     string        `toml:"state" json:"state"`
	Outcome   string        `toml:"outcome,omitempty" json:"outcome,omitempty"`
	Done      time.Time     `toml:"done" json:"done,omitempty"` // no toml omitempty: go-toml/v2 drops a set time.Time with it
}

// Pending-send states: English protocol values (CLI --json, MCP).
const (
	PendingWaiting   = "pending"
	PendingSent      = "sent"
	PendingRejected  = "rejected"
	PendingFailed    = "failed"
	PendingCancelled = "cancelled"
	PendingExpired   = "expired"
)

// PendingSendTTL: a pending entry nobody answered expires, and a finished
// one is dropped, this long after it was created / finished.
const PendingSendTTL = 24 * time.Hour

// DefaultPendingWait is how long an agent's send waits for the answer.
const DefaultPendingWait = 10 * time.Minute

// pendingPoll is the long-poll's interval (tests shorten it).
var pendingPoll = 250 * time.Millisecond

var (
	ErrPendingSendNotFound = errors.New("no such pending send")
	ErrPendingSendClosed   = errors.New("that pending send is no longer waiting")
	ErrPendingStillWaiting = errors.New("still pending")
)

type pendingFile struct {
	Sends []PendingSend `toml:"sends"`
}

// pendingPath is this repository's queue file ("" when no state dir).
func (s *Service) pendingPath(ctx context.Context) (string, error) {
	base := stateBaseDir("pending-sends")
	if base == "" {
		return "", ErrNotesDisabled
	}
	cd, err := s.GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, repoKey(strings.TrimSpace(cd))+".toml"), nil
}

// pendingMutate is the queue's one read-modify-write: lock, read, expire,
// apply fn, write (temp + rename) when anything changed.
func (s *Service) pendingMutate(ctx context.Context, fn func(f *pendingFile) error) error {
	path, err := s.pendingPath(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	release, err := acquirePendingLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	var f pendingFile
	if b, err := os.ReadFile(path); err == nil {
		if err := toml.Unmarshal(b, &f); err != nil {
			_ = os.Rename(path, path+".corrupt-"+clock.Now().Format("20060102150405"))
			f = pendingFile{}
		}
	}
	now := clock.Now()
	kept := f.Sends[:0]
	for _, e := range f.Sends {
		switch {
		case e.State == PendingWaiting && now.Sub(e.Created) > PendingSendTTL:
			e.State, e.Done, e.Outcome = PendingExpired, now, "expired: nobody approved it within 24 h"
		case e.State != PendingWaiting && now.Sub(e.Done) > PendingSendTTL:
			continue
		}
		kept = append(kept, e)
	}
	f.Sends = kept
	if err := fn(&f); err != nil {
		return err
	}
	b, err := toml.Marshal(f)
	if err != nil {
		return err
	}
	if old, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(old, b) {
		return nil // a pure read: nothing to write
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Service) PendingSendAdd(ctx context.Context, req PRSendRequest, requester string) (PendingSend, error) {
	var b [4]byte
	rand.Read(b[:])
	e := PendingSend{ID: hex.EncodeToString(b[:]), Requester: requester, Request: req, Created: clock.Now(), State: PendingWaiting}
	return e, s.pendingMutate(ctx, func(f *pendingFile) error { f.Sends = append(f.Sends, e); return nil })
}

func (s *Service) PendingSends(ctx context.Context) ([]PendingSend, error) {
	var out []PendingSend
	err := s.pendingMutate(ctx, func(f *pendingFile) error { out = append(out, f.Sends...); return nil })
	return out, err
}

func (s *Service) PendingSendGet(ctx context.Context, id string) (PendingSend, error) {
	all, err := s.PendingSends(ctx)
	if err != nil {
		return PendingSend{}, err
	}
	for _, e := range all {
		if e.ID == id {
			return e, nil
		}
	}
	return PendingSend{}, ErrPendingSendNotFound
}

func (s *Service) PendingSendFinish(ctx context.Context, id, state, outcome string) (PendingSend, error) {
	var got PendingSend
	err := s.pendingMutate(ctx, func(f *pendingFile) error {
		for i := range f.Sends {
			if f.Sends[i].ID != id {
				continue
			}
			if f.Sends[i].State != PendingWaiting {
				return ErrPendingSendClosed
			}
			f.Sends[i].State, f.Sends[i].Outcome, f.Sends[i].Done = state, outcome, clock.Now()
			got = f.Sends[i]
			return nil
		}
		return ErrPendingSendNotFound
	})
	return got, err
}

func (s *Service) PendingSendWait(ctx context.Context, id string, timeout time.Duration) (PendingSend, error) {
	deadline := time.Now().Add(timeout)
	for {
		e, err := s.PendingSendGet(ctx, id)
		if err != nil || e.State != PendingWaiting {
			return e, err
		}
		if time.Now().After(deadline) {
			return e, ErrPendingStillWaiting
		}
		select {
		case <-ctx.Done():
			return e, ctx.Err()
		case <-time.After(pendingPoll):
		}
	}
}

// acquirePendingLock takes the queue's lock, retrying a held one for a few
// seconds: two gg processes (an agent's wait, the user's approve) touch the
// file at once, and each holds it only for a read-modify-write.
func acquirePendingLock(ctx context.Context, path string) (func(), error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		release, err := filelock.Acquire(path)
		if err == nil || !errors.Is(err, filelock.ErrHeld) || time.Now().After(deadline) {
			return release, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
