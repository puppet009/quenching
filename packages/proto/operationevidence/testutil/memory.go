package testutil

// MemoryStore is an offline port for deterministic integration verification, never production wiring.
import (
	"context"
	. "github.com/quenching/proto/operationevidence"
	"sort"
	"sync"
)

type MemoryStore struct {
	mu           sync.Mutex
	rows         map[string]Evidence
	keys, hashes map[string]string
	texts        map[string]Text
	Closed       bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]Evidence{}, keys: map[string]string{}, hashes: map[string]string{}, texts: map[string]Text{}}
}
func (s *MemoryStore) Begin(_ context.Context, sid int64, id, key, hash string, e Evidence) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := RequestHash([]any{sid, key})
	if old, ok := s.keys[k]; ok {
		if s.hashes[old] != hash {
			return false, ErrConflict
		}
		return false, nil
	}
	if s.Closed {
		return false, ErrClosed
	}
	if err := Validate(e); err != nil {
		return false, err
	}
	s.keys[k] = id
	s.hashes[id] = hash
	s.rows[id] = Sanitize(e)
	return true, nil
}
func (s *MemoryStore) Append(_ context.Context, e Evidence, texts []Text) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.rows[e.OperationID]
	if !ok {
		return ErrNotFound
	}
	if old.EventID == e.EventID && RequestHash(old) == RequestHash(Sanitize(e)) {
		return nil
	}
	if err := ValidateTransition(old, e); err != nil {
		return err
	}
	for _, t := range texts {
		if t.OperationID != e.OperationID {
			return ErrConflict
		}
	}
	for _, t := range texts {
		s.texts[t.Ref] = t
	}
	s.rows[e.OperationID] = Sanitize(e)
	return nil
}
func (s *MemoryStore) Get(_ context.Context, sid int64, id string) (Evidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.rows[id]
	if !ok || e.SessionID != sid {
		return e, ErrNotFound
	}
	return e, nil
}
func (s *MemoryStore) ByKey(ctx context.Context, sid int64, key string) (Evidence, error) {
	s.mu.Lock()
	id := s.keys[RequestHash([]any{sid, key})]
	s.mu.Unlock()
	return s.Get(ctx, sid, id)
}
func (s *MemoryStore) List(_ context.Context, sid, before int64, limit int) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := Page{Items: []Evidence{}}
	for _, e := range s.rows {
		if e.SessionID == sid {
			p.Items = append(p.Items, e)
		}
	}
	sort.Slice(p.Items, func(i, j int) bool { return p.Items[i].StartedAt.After(p.Items[j].StartedAt) })
	if len(p.Items) > limit {
		p.HasMore = true
		p.Items = p.Items[:limit]
	}
	return p, nil
}
func (s *MemoryStore) Read(_ context.Context, sid int64, ref string) (Text, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.texts[ref]
	if !ok || s.rows[v.OperationID].SessionID != sid {
		return v, ErrNotFound
	}
	return v, nil
}
