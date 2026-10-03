package scripts

import (
	"errors"
	"strings"
	"testing"
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
