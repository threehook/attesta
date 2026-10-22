package presentation

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

var workloadAndMandate = Request{
	Resource: "deploy:prod", PolicyID: "p", IdentityClaim: "workload",
	Credentials: []CredentialQuery{
		{CredentialType: "Workload", Claims: []string{"owner"}},
		{CredentialType: "Mandate", Claims: []string{"scope"}},
	},
}

func TestNewRequestAsksForTheIdentityClaimOfTheRequest(t *testing.T) {
	v := New(Options{PublicURL: "http://backend.test"})
	_, link, err := v.NewRequest(workloadAndMandate)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	var dcql struct {
		Credentials []struct{ Claims []struct{ Path []string } }
	}
	if err := json.Unmarshal([]byte(u.Query().Get("dcql_query")), &dcql); err != nil {
		t.Fatal(err)
	}
	for i, c := range dcql.Credentials {
		var asked []string
		for _, claim := range c.Claims {
			asked = append(asked, strings.Join(claim.Path, "."))
		}
		if !strings.Contains(strings.Join(asked, ","), "workload") || strings.Contains(strings.Join(asked, ","), "email") {
			t.Errorf("credential %d asks for %v, want the workload claim and no email", i, asked)
		}
	}
}

func TestRespondIdentifiesTheHolderByTheRequestedClaim(t *testing.T) {
	w := newWallet(t)
	v, sess := w.verifier(workloadAndMandate)
	const id = "spiffe://corp/ns/cicd/sa/agent"
	got, err := v.Respond(context.Background(), "req-1", form(map[string]string{
		"credential":   w.present("Workload", map[string]any{"workload": id, "owner": "platform"}, sess.nonce, sess.responseURI),
		"credential_2": w.present("Mandate", map[string]any{"workload": id, "scope": "deploy:prod"}, sess.nonce, sess.responseURI),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject.ID != id || got.Subject.Claim != "workload" || got.Subject.Issuer != w.did {
		t.Errorf("Subject = %+v, want the workload id, claim workload and the issuer", got.Subject)
	}
}

func TestRespondComparesAnIdentifierThatIsNotAnEmailExactly(t *testing.T) {
	w := newWallet(t)
	v, sess := w.verifier(workloadAndMandate)
	_, err := v.Respond(context.Background(), "req-1", form(map[string]string{
		"credential":   w.present("Workload", map[string]any{"workload": "spiffe://corp/ns/cicd/sa/Agent", "owner": "platform"}, sess.nonce, sess.responseURI),
		"credential_2": w.present("Mandate", map[string]any{"workload": "spiffe://corp/ns/cicd/sa/agent", "scope": "deploy:prod"}, sess.nonce, sess.responseURI),
	}))
	if !errors.Is(err, ErrInvalidPresentation) || !strings.Contains(err.Error(), "same holder") {
		t.Errorf("error = %v, want ErrInvalidPresentation about the same holder", err)
	}
}

func TestRespondRequiresTheIdentityClaimOfTheRequestNotTheEmail(t *testing.T) {
	w := newWallet(t)
	v, sess := w.verifier(workloadAndMandate)
	_, err := v.Respond(context.Background(), "req-1", form(map[string]string{
		"credential":   w.present("Workload", map[string]any{"email": "a@example.com", "owner": "platform"}, sess.nonce, sess.responseURI),
		"credential_2": w.present("Mandate", map[string]any{"email": "a@example.com", "scope": "deploy:prod"}, sess.nonce, sess.responseURI),
	}))
	if !errors.Is(err, ErrInvalidPresentation) || !strings.Contains(err.Error(), "workload") {
		t.Errorf("error = %v, want ErrInvalidPresentation mentioning the workload claim", err)
	}
}
