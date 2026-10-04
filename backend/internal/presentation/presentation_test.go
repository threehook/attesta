package presentation

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"attesta/backend/internal/sdjwt"
)

// recorded is a presentation captured from a Credo wallet (see internal/sdjwt/testdata): the session below is built to match its audience, nonce
// and state, and the clock is set to when it was made.
type recording struct{ Nonce, State, VPToken string }

type recorded struct {
	Issuer, Audience string
	IssuedAt         int64
	// Full discloses the degree and the email; WithoutEmail only the degree.
	Full, WithoutEmail recording
}

func loadRecorded(t *testing.T) recorded {
	t.Helper()
	raw, err := os.ReadFile("../sdjwt/testdata/credo-presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	var r recorded
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func newRecordedVerifier(t *testing.T, r recorded, rec recording, request Request) *Verifier {
	t.Helper()
	v := New(Options{PublicURL: "http://backend.test", Keys: sdjwt.DIDKey{}, Now: func() time.Time { return time.Unix(r.IssuedAt, 0) }})
	v.sessions.put("req-1", session{request: request, nonce: rec.Nonce, state: rec.State, responseURI: r.Audience[len("redirect_uri:"):]})
	return v
}

func formOf(rec recording) url.Values {
	return url.Values{"vp_token": {rec.VPToken}, "state": {rec.State}}
}

var diploma = Request{Resource: "vault", PolicyID: "p1", CredentialType: "Diploma", Claims: []string{"degree"}}

func TestNewRequest(t *testing.T) {
	v := New(Options{PublicURL: "http://backend.test/", Keys: sdjwt.DIDKey{}})

	id, link, err := v.NewRequest(diploma)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "openid4vp" {
		t.Fatalf("link = %q, want an openid4vp:// link", link)
	}
	q := u.Query()
	responseURI := "http://backend.test/v1/authorize/requests/" + id + "/response"
	for key, want := range map[string]string{
		"response_type": "vp_token", "response_mode": "direct_post", "response_uri": responseURI, "client_id": "redirect_uri:" + responseURI,
	} {
		if q.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, q.Get(key), want)
		}
	}
	if q.Get("nonce") == "" || q.Get("state") == "" || q.Get("nonce") == q.Get("state") {
		t.Errorf("nonce %q and state %q must be set and differ", q.Get("nonce"), q.Get("state"))
	}

	var dcql struct {
		Credentials []struct {
			ID, Format string
			Meta       struct {
				VCTValues []string `json:"vct_values"`
			}
			Claims []struct{ Path []string }
		}
	}
	if err := json.Unmarshal([]byte(q.Get("dcql_query")), &dcql); err != nil || len(dcql.Credentials) != 1 {
		t.Fatalf("dcql_query = %s", q.Get("dcql_query"))
	}
	c := dcql.Credentials[0]
	if c.Format != "dc+sd-jwt" || len(c.Meta.VCTValues) != 1 || c.Meta.VCTValues[0] != "Diploma" || len(c.Claims) != 2 || c.Claims[0].Path[0] != "degree" || c.Claims[1].Path[0] != "email" {
		t.Errorf("credential query = %+v, want a Diploma asking for degree and the identifying email", c)
	}

	other, _, _ := v.NewRequest(diploma)
	if other == id {
		t.Error("two requests got the same id")
	}
}

func TestNewRequestAlwaysAsksForTheEmail(t *testing.T) {
	v := New(Options{PublicURL: "http://backend.test", Keys: sdjwt.DIDKey{}})
	for name, claims := range map[string][]string{"no claims": nil, "email already asked for": {"email", "degree"}} {
		_, link, err := v.NewRequest(Request{CredentialType: "Diploma", Claims: claims})
		if err != nil {
			t.Fatal(name, err)
		}
		u, _ := url.Parse(link)
		var dcql struct {
			Credentials []struct{ Claims []struct{ Path []string } }
		}
		if err := json.Unmarshal([]byte(u.Query().Get("dcql_query")), &dcql); err != nil {
			t.Fatal(name, err)
		}
		emails := 0
		for _, c := range dcql.Credentials[0].Claims {
			if c.Path[0] == "email" {
				emails++
			}
		}
		if emails != 1 {
			t.Errorf("%s: the email is asked for %d times, want once", name, emails)
		}
	}
}

func TestRespond(t *testing.T) {
	r := loadRecorded(t)
	v := newRecordedVerifier(t, r, r.Full, diploma)

	got, err := v.Respond(context.Background(), "req-1", formOf(r.Full))
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if got.Issuer != r.Issuer || got.Type != "Diploma" || got.Claims["degree"] != "Mathematics" || got.Request.Resource != "vault" {
		t.Errorf("Presented = %+v", got)
	}
	if got.Subject != (Subject{Issuer: r.Issuer, Email: "ada@example.com"}) {
		t.Errorf("Subject = %+v, want the issuer and the disclosed email", got.Subject)
	}
}

func TestRespondRequiresTheEmail(t *testing.T) {
	r := loadRecorded(t)
	v := newRecordedVerifier(t, r, r.WithoutEmail, diploma)

	_, err := v.Respond(context.Background(), "req-1", formOf(r.WithoutEmail))
	if !errors.Is(err, ErrInvalidPresentation) || !strings.Contains(err.Error(), "email") {
		t.Errorf("error = %v, want ErrInvalidPresentation mentioning the email", err)
	}
}

func TestRespondRequiresEveryRequestedClaim(t *testing.T) {
	r := loadRecorded(t)
	v := newRecordedVerifier(t, r, r.Full, Request{CredentialType: "Diploma", Claims: []string{"degree", "department"}})

	_, err := v.Respond(context.Background(), "req-1", formOf(r.Full))
	if !errors.Is(err, ErrInvalidPresentation) || !strings.Contains(err.Error(), "department") {
		t.Errorf("error = %v, want ErrInvalidPresentation mentioning the missing claim", err)
	}
}

func TestRespondKeepsOnlyTheAskedClaims(t *testing.T) {
	r := loadRecorded(t)
	v := newRecordedVerifier(t, r, r.Full, Request{CredentialType: "Diploma"})

	got, err := v.Respond(context.Background(), "req-1", formOf(r.Full))
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if len(got.Claims) != 1 || got.Claims["email"] != "ada@example.com" {
		t.Errorf("Claims = %v, want only the email: the degree was disclosed but not asked for", got.Claims)
	}
}

func TestRespondRejects(t *testing.T) {
	r := loadRecorded(t)
	good := formOf(r.Full)
	with := func(k, v string) url.Values {
		f := formOf(r.Full)
		f.Set(k, v)
		return f
	}
	cases := map[string]struct {
		form    url.Values
		request Request
		mutate  func(v *Verifier)
	}{
		"wrong state":              {form: with("state", "other"), request: diploma},
		"vp_token not JSON":        {form: with("vp_token", "nope"), request: diploma},
		"no presentation":          {form: with("vp_token", `{"credential":[]}`), request: diploma},
		"unknown query id":         {form: with("vp_token", `{"other":["a~"]}`), request: diploma},
		"another credential type":  {form: good, request: Request{CredentialType: "Passport"}},
		"another nonce":            {form: good, request: diploma, mutate: func(v *Verifier) { v.sessions.m["req-1"].nonce = "other" }},
		"another verifier address": {form: good, request: diploma, mutate: func(v *Verifier) { v.sessions.m["req-1"].responseURI = "http://evil.test/response" }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			v := newRecordedVerifier(t, r, r.Full, tc.request)
			if tc.mutate != nil {
				tc.mutate(v)
			}
			_, err := v.Respond(context.Background(), "req-1", tc.form)
			if !errors.Is(err, ErrInvalidPresentation) {
				t.Errorf("error = %v, want ErrInvalidPresentation", err)
			}
		})
	}
}

func TestRespondOnlyOnce(t *testing.T) {
	r := loadRecorded(t)
	v := newRecordedVerifier(t, r, r.Full, diploma)
	form := formOf(r.Full)

	if _, err := v.Respond(context.Background(), "req-1", form); err != nil {
		t.Fatalf("first Respond: %v", err)
	}
	if _, err := v.Respond(context.Background(), "req-1", form); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("second Respond error = %v, want ErrAlreadyAnswered", err)
	}
}

func TestRespondConsumesRequestOnFailure(t *testing.T) {
	r := loadRecorded(t)
	v := newRecordedVerifier(t, r, r.Full, diploma)

	if _, err := v.Respond(context.Background(), "req-1", url.Values{"vp_token": {"bad"}, "state": {r.Full.State}}); err == nil {
		t.Fatal("want an error for a bad vp_token")
	}
	if _, err := v.Respond(context.Background(), "req-1", formOf(r.Full)); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("Respond after a failed attempt: error = %v, want ErrAlreadyAnswered", err)
	}
}

func TestRespondUnknownAndExpired(t *testing.T) {
	r := loadRecorded(t)
	form := formOf(r.Full)

	v := newRecordedVerifier(t, r, r.Full, diploma)
	if _, err := v.Respond(context.Background(), "nope", form); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("unknown id: error = %v, want ErrUnknownRequest", err)
	}

	clock := time.Unix(r.IssuedAt, 0)
	v = New(Options{PublicURL: "http://backend.test", Keys: sdjwt.DIDKey{}, Now: func() time.Time { return clock }})
	v.sessions.put("req-1", session{request: diploma, nonce: r.Full.Nonce, state: r.Full.State, responseURI: r.Audience[len("redirect_uri:"):]})
	clock = clock.Add(6 * time.Minute)
	if _, err := v.Respond(context.Background(), "req-1", form); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("expired request: error = %v, want ErrUnknownRequest", err)
	}
}

func TestOutcome(t *testing.T) {
	v := New(Options{PublicURL: "http://backend.test", Keys: sdjwt.DIDKey{}})
	id, _, _ := v.NewRequest(diploma)

	if o, err := v.Outcome(id); err != nil || o != nil {
		t.Fatalf("before an answer: outcome = %v, err = %v, want nil, nil", o, err)
	}
	v.Complete(id, Outcome{Allow: true, Reason: "ok"})
	if o, err := v.Outcome(id); err != nil || o == nil || !o.Allow || o.Reason != "ok" {
		t.Errorf("after Complete: outcome = %v, err = %v", o, err)
	}
	if _, err := v.Outcome("nope"); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("unknown id: error = %v, want ErrUnknownRequest", err)
	}
}
