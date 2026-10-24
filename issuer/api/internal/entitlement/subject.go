package entitlement

import (
	"errors"
	"fmt"
	"strings"
)

// SubjectKind is the kind of holder that an entitlement is for.
type SubjectKind string

const (
	Person   SubjectKind = "person"
	Workload SubjectKind = "workload"
)

// Subject is who holds an entitlement: a person is identified by email, a workload by SPIFFE ID. Build one with PersonOf or WorkloadOf.
type Subject struct {
	Kind     SubjectKind
	Email    string
	SPIFFEID string
}

// PersonOf is the subject for an email address, which is compared in lowercase.
func PersonOf(email string) Subject {
	return Subject{Kind: Person, Email: strings.ToLower(strings.TrimSpace(email))}
}

// WorkloadOf is the subject for a SPIFFE ID.
func WorkloadOf(spiffeID string) Subject {
	return Subject{Kind: Workload, SPIFFEID: spiffeID}
}

// subjectOf reads the two columns: exactly one of them is filled.
func subjectOf(email, spiffeID string) Subject {
	return map[bool]Subject{
		true:  {Kind: Workload, SPIFFEID: spiffeID},
		false: {Kind: Person, Email: email},
	}[spiffeID != ""]
}

// kind is what differs per kind of subject.
type kind struct {
	column   string // the entitlements column that holds the identifier
	mailable bool
	id       func(Subject) string // the identifier of this kind
	other    func(Subject) string // what must be empty for this kind
}

var kinds = map[SubjectKind]kind{
	Person:   {column: "email", mailable: true, id: func(s Subject) string { return s.Email }, other: func(s Subject) string { return s.SPIFFEID }},
	Workload: {column: "workload", id: func(s Subject) string { return s.SPIFFEID }, other: func(s Subject) string { return s.Email }},
}

// Validate reports why the subject cannot hold an entitlement: it has the identifier of its kind and no other.
func (s Subject) Validate() error {
	k, ok := kinds[s.Kind]
	if !ok {
		return errors.New("unknown kind of subject")
	}

	if k.id(s) == "" || k.other(s) != "" {
		return fmt.Errorf("a %s is identified by %s only", s.Kind, k.column)
	}

	return nil
}

// Mailable reports whether the issuer reaches the subject by mail, so it can send a QR code. A workload asks for a new link itself.
func (s Subject) Mailable() bool { return kinds[s.Kind].mailable }

// column is the entitlements column that holds the subject's identifier; key is the value in it.
func (s Subject) column() string { return kinds[s.Kind].column }

func (s Subject) key() string { return kinds[s.Kind].id(s) }
