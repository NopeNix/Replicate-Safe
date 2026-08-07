package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State tracks sync progress so we only fetch new predictions on each run.
type State struct {
	LastCreatedAt time.Time   `json:"last_created_at,omitempty"`
	SeenIDs       map[string]struct{} `json:"seen_ids"`

	mu sync.Mutex `json:"-"`
}

func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{SeenIDs: map[string]struct{}{}}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	s := &State{SeenIDs: map[string]struct{}{}}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.SeenIDs == nil {
		s.SeenIDs = map[string]struct{}{}
	}
	return s, nil
}

// Save writes state atomically: tmp file in same dir, fsync, rename.
func (s *State) Save(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// HasSeen returns true if the prediction id was already processed.
func (s *State) HasSeen(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.SeenIDs[id]
	return ok
}

// MarkSeen records the prediction id and updates last_created_at.
func (s *State) MarkSeen(id string, createdAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SeenIDs[id] = struct{}{}
	if createdAt.After(s.LastCreatedAt) {
		s.LastCreatedAt = createdAt
	}
}
