package presentation

import (
	"errors"
	"sync"
	"time"
)

// Outcome is the decision on a presentation, kept so the application that started the request can fetch it.
type Outcome struct {
	Allow  bool
	Reason string
	// Subject is set when the presentation was allowed.
	Subject *Subject
}

type session struct {
	request     Request
	nonce       string
	state       string
	responseURI string
	responded   bool
	outcome     *Outcome
	deadline    time.Time
}

// sessions holds outstanding requests. A request can be answered once; its outcome stays readable until the request expires.
type sessions struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	m   map[string]*session
}

func newSessions(ttl time.Duration, now func() time.Time) *sessions {
	return &sessions{ttl: ttl, now: now, m: make(map[string]*session)}
}

func (s *sessions) put(id string, sess session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.m {
		if now.After(v.deadline) {
			delete(s.m, k)
		}
	}
	sess.deadline = now.Add(s.ttl)
	s.m[id] = &sess
}

// claim marks the request answered and returns it; a second claim fails.
func (s *sessions) claim(id string) (session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[id]
	if !ok || s.now().After(sess.deadline) {
		return session{}, ErrUnknownRequest
	}
	if sess.responded {
		return session{}, ErrAlreadyAnswered
	}
	sess.responded = true
	return *sess, nil
}

func (s *sessions) complete(id string, o Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.m[id]; ok {
		sess.outcome = &o
	}
}

func (s *sessions) outcome(id string) (*Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[id]
	if !ok || s.now().After(sess.deadline) {
		return nil, ErrUnknownRequest
	}
	return sess.outcome, nil
}

var (
	ErrUnknownRequest  = errors.New("unknown or expired request")
	ErrAlreadyAnswered = errors.New("request was already answered")
)
