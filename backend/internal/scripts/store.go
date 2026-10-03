// Package scripts holds the raw Gno source of authorization policies, loaded from disk at startup and/or hot-deployed later via the admin API. It
// knows nothing about gnovm — that's internal/authz's job; this package only stores source text and validates it through a caller-supplied validator
// before accepting it.
package scripts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Validator checks that policy source is well-formed before it's stored. internal/authz.GnoVM satisfies this via its Validate method.
type Validator interface {
	Validate(source string) error
}

type Store struct {
	validator Validator

	mu       sync.RWMutex
	policies map[string]string // policy ID -> .gno source
}

func NewStore(validator Validator) *Store {
	return &Store{validator: validator, policies: make(map[string]string)}
}

// LoadDir loads every *.gno file in dir, using the filename (without extension) as the policy ID. It fails fast on the first invalid file — this runs
// at startup, where surfacing a bad policy immediately is preferable to starting with a partially loaded policy set.
func (s *Store) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read policies dir %q: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gno") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read policy %q: %w", path, err)
		}
		id := strings.TrimSuffix(entry.Name(), ".gno")
		if err := s.Put(id, string(source)); err != nil {
			return fmt.Errorf("load policy %q: %w", id, err)
		}
	}
	return nil
}

// Put validates source and, if valid, installs or replaces the policy at id. This is what both startup loading and the admin hot-deploy endpoint go
// through, so a bad policy can never make it into the store.
func (s *Store) Put(id, source string) error {
	if err := s.validator.Validate(source); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[id] = source
	return nil
}

func (s *Store) Get(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, ok := s.policies[id]
	return source, ok
}

func (s *Store) IDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.policies))
	for id := range s.policies {
		ids = append(ids, id)
	}
	return ids
}
