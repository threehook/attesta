// Package scripts holds the raw Gno source of authorization policies, loaded from disk at startup and/or hot-deployed later via the admin API. It
// knows nothing about gnovm — that's internal/authz's job; this package only stores source text and validates it through a caller-supplied validator
// before accepting it.
package scripts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Validator checks that policy source is well-formed before it's stored. internal/authz.GnoVM satisfies this via its Validate method.
type Validator interface {
	Validate(source string) error
}

type Store struct {
	validator Validator

	// syncMu serializes LoadDir and Sync, which both read the policy directory and update the bookkeeping below.
	syncMu sync.Mutex

	mu       sync.RWMutex
	policies map[string]string // policy ID -> .gno source
	// fromDir holds the policies whose current version came from the policy directory. Only those are removed when their file disappears: a policy
	// deployed with Put (the admin API) leaves this set, so the directory never takes it away.
	fromDir map[string]bool
	// seen is the hash of the last version of each directory file that was processed, valid or not. An unchanged file is not processed again, so a
	// rejected file is reported once and a file never replaces a policy that was deployed with Put unless the file itself changes.
	seen map[string]string
}

func NewStore(validator Validator) *Store {
	return &Store{validator: validator, policies: make(map[string]string), fromDir: make(map[string]bool), seen: make(map[string]string)}
}

// LoadDir loads every *.gno file in dir, using the filename (without extension) as the policy ID. It fails fast on the first invalid file — this runs
// at startup, where surfacing a bad policy immediately is preferable to starting with a partially loaded policy set. Sync keeps the store up to date
// afterwards.
func (s *Store) LoadDir(dir string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	files, err := readDir(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.err != nil {
			return f.err
		}
		if err := s.validator.Validate(f.source); err != nil {
			return fmt.Errorf("load policy %q: invalid policy: %w", f.id, err)
		}
		s.installFromDir(f)
	}
	return nil
}

// SyncResult is what one Sync changed. A Sync that found nothing new has an empty result.
type SyncResult struct {
	Installed []string // policies added or replaced because their file is new or changed
	Removed   []string // policies removed because their file is gone
	// Failed maps the policy ID to why its new file was rejected. The previous version, if any, stays in use.
	Failed map[string]error
	// Err is set when the directory could not be read; nothing was changed.
	Err error
}

// Empty reports that the Sync changed and rejected nothing.
func (r SyncResult) Empty() bool {
	return len(r.Installed) == 0 && len(r.Removed) == 0 && len(r.Failed) == 0 && r.Err == nil
}

// Sync brings the policies that come from dir in line with its files, without a restart: a new or changed file is validated and installed, a file
// that is gone takes its policy with it, and an invalid file is rejected and leaves the previous version in use.
func (s *Store) Sync(dir string) SyncResult {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	var result SyncResult
	files, err := readDir(dir)
	if err != nil {
		result.Err = err
		return result
	}

	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f.id] = true
		if f.err != nil {
			continue // unreadable right now, perhaps mid-update; the next Sync tries again
		}
		s.mu.RLock()
		unchanged := s.seen[f.id] == f.hash
		s.mu.RUnlock()
		if unchanged {
			continue
		}
		if err := s.validator.Validate(f.source); err != nil {
			s.mu.Lock()
			s.seen[f.id] = f.hash
			s.mu.Unlock()
			if result.Failed == nil {
				result.Failed = make(map[string]error)
			}
			result.Failed[f.id] = fmt.Errorf("invalid policy: %w", err)
			continue
		}
		s.installFromDir(f)
		result.Installed = append(result.Installed, f.id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.seen {
		if present[id] {
			continue
		}
		delete(s.seen, id)
		if s.fromDir[id] {
			delete(s.policies, id)
			delete(s.fromDir, id)
			result.Removed = append(result.Removed, id)
		}
	}
	sort.Strings(result.Removed)
	return result
}

// Watch calls Sync(dir) every interval until ctx is done, and report with each result that changed or rejected something.
func (s *Store) Watch(ctx context.Context, dir string, interval time.Duration, report func(SyncResult)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if result := s.Sync(dir); !result.Empty() {
				report(result)
			}
		}
	}
}

type policyFile struct {
	id, source, hash string
	err              error // the file could not be read
}

// readDir reads every *.gno file in dir, sorted by policy ID.
func readDir(dir string) ([]policyFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read policies dir %q: %w", dir, err)
	}
	var files []policyFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gno") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		f := policyFile{id: strings.TrimSuffix(entry.Name(), ".gno")}
		source, err := os.ReadFile(path)
		if err != nil {
			f.err = fmt.Errorf("read policy %q: %w", path, err)
		} else {
			sum := sha256.Sum256(source)
			f.source, f.hash = string(source), hex.EncodeToString(sum[:])
		}
		files = append(files, f)
	}
	return files, nil
}

func (s *Store) installFromDir(f policyFile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[f.id] = f.source
	s.fromDir[f.id] = true
	s.seen[f.id] = f.hash
}

// Put validates source and, if valid, installs or replaces the policy at id: the admin hot-deploy path. Policies from the directory go through the
// same validation, so a bad policy can never make it into the store. A policy deployed this way is no longer managed by the directory.
func (s *Store) Put(id, source string) error {
	if err := s.validator.Validate(source); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[id] = source
	delete(s.fromDir, id)
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
