package taskhist

import (
	"fmt"
	"sync"
)

// MemStore is the in-memory Store: the fallback when the file store cannot
// be written (spec: tasks still run, one notice, records in memory).
type MemStore struct {
	mu      sync.Mutex
	recs    []Record
	results map[string]string
	tails   map[string]string
}

func NewMemStore() *MemStore {
	return &MemStore{results: map[string]string{}, tails: map[string]string{}}
}

func (m *MemStore) List() ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Record(nil), m.recs...), nil
}

func (m *MemStore) Add(r Record, result, tail string) error {
	if r.ID == "" {
		return fmt.Errorf("taskhist: a record needs an id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[r.ID], m.tails[r.ID] = result, TrimTail(tail)
	out := []Record{r}
	for _, x := range m.recs {
		if x.ID != r.ID {
			out = append(out, x)
		}
	}
	for _, x := range out[min(len(out), Max):] {
		delete(m.results, x.ID)
		delete(m.tails, x.ID)
	}
	m.recs = out[:min(len(out), Max)]
	return nil
}

func (m *MemStore) Result(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.results[id], nil
}

func (m *MemStore) Tail(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tails[id], nil
}

func (m *MemStore) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.recs[:0:0]
	for _, r := range m.recs {
		if r.ID != id {
			out = append(out, r)
		}
	}
	m.recs = out
	delete(m.results, id)
	delete(m.tails, id)
	return nil
}
