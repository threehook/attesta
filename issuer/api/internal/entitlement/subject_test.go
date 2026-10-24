package entitlement_test

import (
	"attesta/issuer/internal/entitlement"
	"testing"
)

func TestASubjectHasTheIdentifierOfItsKindOnly(t *testing.T) {
	valid := []entitlement.Subject{entitlement.PersonOf(" Jerry@Example.com "), entitlement.WorkloadOf("spiffe://corp.test/ns/a/sa/b")}
	for _, s := range valid {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}

	invalid := []entitlement.Subject{
		{},
		{Kind: "robot", Email: "a@b.c"},
		entitlement.PersonOf(""), entitlement.WorkloadOf(""),
		{Kind: entitlement.Person, Email: "a@b.c", SPIFFEID: "spiffe://x/y"},
		{Kind: entitlement.Workload, Email: "a@b.c", SPIFFEID: "spiffe://x/y"},
	}
	for _, s := range invalid {
		if s.Validate() == nil {
			t.Errorf("%+v was accepted", s)
		}
	}
}

func TestAPersonIsComparedInLowercaseAndOnlyAPersonIsMailable(t *testing.T) {
	if entitlement.PersonOf(" Jerry@Example.com ") != entitlement.PersonOf("jerry@example.com") {
		t.Error("the same address in another case is another person")
	}

	if !entitlement.PersonOf("a@b.c").Mailable() || entitlement.WorkloadOf("spiffe://x/y").Mailable() {
		t.Error("a person is reached by mail, a workload is not")
	}
}
