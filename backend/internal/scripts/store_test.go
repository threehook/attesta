package scripts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// rejectsFoo is a stub Validator for testing Store in isolation from gnovm: it rejects any source containing "foo", and otherwise accepts.
type rejectsFoo struct{}

func (rejectsFoo) Validate(source string) error {
	if strings.Contains(source, "foo") {
		return errors.New("contains foo")
	}
	return nil
}

func TestStorePutGet(t *testing.T) {
	s := NewStore(rejectsFoo{})

	if err := s.Put("ok", "package policy"); err != nil {
		t.Fatalf("Put valid source: %v", err)
	}
	got, ok := s.Get("ok")
	if !ok || got != "package policy" {
		t.Fatalf("Get(%q) = %q, %v; want %q, true", "ok", got, ok, "package policy")
	}

	if err := s.Put("bad", "foo"); err == nil {
		t.Fatal("Put invalid source: want error, got nil")
	}
	if _, ok := s.Get("bad"); ok {
		t.Fatal("Get after failed Put: want not found")
	}
}

func TestStorePutReplaces(t *testing.T) {
	s := NewStore(rejectsFoo{})

	_ = s.Put("p", "v1")
	_ = s.Put("p", "v2")

	got, _ := s.Get("p")
	if got != "v2" {
		t.Fatalf("Get after replace = %q, want %q", got, "v2")
	}
}

func writePolicy(t *testing.T, dir, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustGet(t *testing.T, s *Store, id, want string) {
	t.Helper()
	if got, ok := s.Get(id); !ok || got != want {
		t.Errorf("Get(%q) = %q, %v; want %q, true", id, got, ok, want)
	}
}

func mustBeAbsent(t *testing.T, s *Store, id string) {
	t.Helper()
	if got, ok := s.Get(id); ok {
		t.Errorf("Get(%q) = %q, true; want it absent", id, got)
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "a.gno", "package a")
	writePolicy(t, dir, "b.gno", "package b")
	writePolicy(t, dir, "notes.txt", "not a policy")
	s := NewStore(rejectsFoo{})

	if err := s.LoadDir(dir); err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	mustGet(t, s, "a", "package a")
	mustGet(t, s, "b", "package b")
	if got := len(s.IDs()); got != 2 {
		t.Errorf("IDs = %v, want the two .gno files", s.IDs())
	}

	writePolicy(t, dir, "c.gno", "foo")
	if err := NewStore(rejectsFoo{}).LoadDir(dir); err == nil || !strings.Contains(err.Error(), "c") {
		t.Errorf("LoadDir with an invalid file: error = %v, want one naming the policy", err)
	}
	if err := s.LoadDir(filepath.Join(dir, "missing")); err == nil {
		t.Error("LoadDir of a missing directory: want an error")
	}
}

func TestSyncFollowsTheDirectory(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "a.gno", "a v1")
	s := NewStore(rejectsFoo{})
	if err := s.LoadDir(dir); err != nil {
		t.Fatal(err)
	}

	if r := s.Sync(dir); !r.Empty() {
		t.Errorf("Sync with nothing changed = %+v, want empty", r)
	}

	writePolicy(t, dir, "a.gno", "a v2")
	writePolicy(t, dir, "b.gno", "b v1")
	r := s.Sync(dir)
	if !reflect.DeepEqual(r.Installed, []string{"a", "b"}) || len(r.Removed) != 0 || len(r.Failed) != 0 {
		t.Errorf("Sync after a change and an addition = %+v", r)
	}
	mustGet(t, s, "a", "a v2")
	mustGet(t, s, "b", "b v1")

	if err := os.Remove(filepath.Join(dir, "b.gno")); err != nil {
		t.Fatal(err)
	}
	r = s.Sync(dir)
	if !reflect.DeepEqual(r.Removed, []string{"b"}) || len(r.Installed) != 0 {
		t.Errorf("Sync after a removal = %+v", r)
	}
	mustBeAbsent(t, s, "b")
	mustGet(t, s, "a", "a v2")
}

func TestSyncRejectsAnInvalidFileOnceAndKeepsTheOldVersion(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "a.gno", "a v1")
	s := NewStore(rejectsFoo{})
	if err := s.LoadDir(dir); err != nil {
		t.Fatal(err)
	}

	writePolicy(t, dir, "a.gno", "a foo")
	r := s.Sync(dir)
	if len(r.Failed) != 1 || r.Failed["a"] == nil || len(r.Installed) != 0 {
		t.Fatalf("Sync with an invalid change = %+v, want a failure for a", r)
	}
	mustGet(t, s, "a", "a v1")
	if r := s.Sync(dir); !r.Empty() {
		t.Errorf("Sync of the same rejected file again = %+v, want empty: it is reported once", r)
	}

	writePolicy(t, dir, "a.gno", "a v3")
	if r := s.Sync(dir); !reflect.DeepEqual(r.Installed, []string{"a"}) {
		t.Errorf("Sync after the fix = %+v", r)
	}
	mustGet(t, s, "a", "a v3")

	writePolicy(t, dir, "new.gno", "new foo")
	if r := s.Sync(dir); len(r.Failed) != 1 {
		t.Errorf("Sync with an invalid new file = %+v", r)
	}
	mustBeAbsent(t, s, "new")
}

func TestSyncLeavesPoliciesDeployedWithPutAlone(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "a.gno", "a from file")
	s := NewStore(rejectsFoo{})
	if err := s.LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("hot", "hot v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("a", "a hot deployed"); err != nil {
		t.Fatal(err)
	}

	if r := s.Sync(dir); !r.Empty() {
		t.Errorf("Sync with an unchanged directory = %+v, want empty", r)
	}
	mustGet(t, s, "hot", "hot v1")
	mustGet(t, s, "a", "a hot deployed")

	if err := os.Remove(filepath.Join(dir, "a.gno")); err != nil {
		t.Fatal(err)
	}
	if r := s.Sync(dir); len(r.Removed) != 0 {
		t.Errorf("Sync after the file of a hot-deployed policy was removed = %+v, want it kept", r)
	}
	mustGet(t, s, "a", "a hot deployed")
	mustGet(t, s, "hot", "hot v1")

	writePolicy(t, dir, "a.gno", "a file v2")
	s.Sync(dir)
	mustGet(t, s, "a", "a file v2")
}

func TestSyncKeepsEverythingWhenTheDirectoryCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "a.gno", "a v1")
	s := NewStore(rejectsFoo{})
	if err := s.LoadDir(dir); err != nil {
		t.Fatal(err)
	}

	r := s.Sync(filepath.Join(dir, "gone"))
	if r.Err == nil || r.Empty() {
		t.Errorf("Sync of a missing directory = %+v, want an error", r)
	}
	mustGet(t, s, "a", "a v1")
}

func TestWatchAppliesChangesUntilCancelled(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "a.gno", "a v1")
	s := NewStore(rejectsFoo{})
	if err := s.LoadDir(dir); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	reported := make(chan SyncResult, 4)
	done := make(chan struct{})
	go func() {
		s.Watch(ctx, dir, 10*time.Millisecond, func(r SyncResult) { reported <- r })
		close(done)
	}()

	writePolicy(t, dir, "a.gno", "a v2")
	select {
	case r := <-reported:
		if !reflect.DeepEqual(r.Installed, []string{"a"}) {
			t.Errorf("reported %+v, want a installed", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not pick up the change")
	}
	mustGet(t, s, "a", "a v2")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not stop after cancel")
	}
}
