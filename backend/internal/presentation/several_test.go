package presentation

import (
	"attesta/backend/internal/status"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/threehook/sdjwt-go"
)

// wallet makes presentations the way a wallet does, for a request with several credentials.
type wallet struct {
	t      *testing.T
	issuer ed25519.PrivateKey
	did    string
	now    time.Time
}

func newWallet(t *testing.T) wallet {
	t.Helper()

	pub, key, _ := ed25519.GenerateKey(rand.Reader)

	return wallet{t: t, issuer: key, did: sdjwt.DIDKeyFromEd25519(pub), now: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
}

// present issues a credential of the type with the claims, bound to a fresh key, and presents all of it for the verifier's nonce.
func (w wallet) present(credType string, claims map[string]any, nonce string, responseURI string) string {
	w.t.Helper()

	pub, holder, _ := ed25519.GenerateKey(rand.Reader)

	issued, err := sdjwt.Issue(w.issuer, sdjwt.Credential{
		Type: credType, HolderKey: map[string]any{"kty": "OKP", "crv": "Ed25519", "x": base64.RawURLEncoding.EncodeToString(pub)},
		ExpiresAt: w.now.Add(time.Hour), Now: func() time.Time { return w.now }, Disclosable: claims,
	})
	if err != nil {
		w.t.Fatal(err)
	}

	sum := sha256.Sum256([]byte(issued))
	kb := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"aud": clientID(responseURI), "nonce": nonce, "iat": w.now.Unix(), "sd_hash": base64.RawURLEncoding.EncodeToString(sum[:]),
	})
	kb.Header["typ"] = "kb+jwt"

	signed, err := kb.SignedString(holder)
	if err != nil {
		w.t.Fatal(err)
	}

	return issued + signed
}

func (w wallet) verifier(request Request) (*Verifier, session) {
	v := New(Options{PublicURL: "http://backend.test", Keys: sdjwt.DIDKey{}, Status: goodStanding{}, Now: func() time.Time { return w.now }})
	sess := session{request: request, nonce: "n-1", state: "s-1", responseURI: "http://backend.test/v1/authorize/requests/req-1/response"}
	v.sessions.put("req-1", sess)

	return v, sess
}

func form(tokens map[string]string) url.Values {
	vp := map[string][]string{}
	for id, token := range tokens {
		vp[id] = []string{token}
	}

	raw, err := json.Marshal(vp)
	if err != nil {
		panic(err)
	}

	return url.Values{"vp_token": {string(raw)}, "state": {"s-1"}}
}

var employeeAndCertificate = Request{
	Resource: "request_laadpaal", PolicyID: "p",
	Credentials: []CredentialQuery{
		{CredentialType: "Employee", Claims: []string{"department", "roles"}},
		{CredentialType: "Certificate", Claims: []string{"course"}},
	},
}

func TestNewRequestAsksForEveryCredential(t *testing.T) {
	v := New(Options{PublicURL: "http://backend.test", Keys: sdjwt.DIDKey{}})

	_, link, err := v.NewRequest(employeeAndCertificate)
	if err != nil {
		t.Fatal(err)
	}

	u, _ := url.Parse(link)

	var dcql dcqlQuery
	if err := json.Unmarshal([]byte(u.Query().Get("dcql_query")), &dcql); err != nil {
		t.Fatal(err)
	}

	if len(dcql.Credentials) != 2 || dcql.Credentials[0].ID != "credential" || dcql.Credentials[1].ID != "credential_2" ||
		dcql.Credentials[0].Meta.VCTValues[0] != "Employee" || dcql.Credentials[1].Meta.VCTValues[0] != "Certificate" {
		t.Fatalf("dcql %+v", dcql)
	}

	second := make([]string, 0, len(dcql.Credentials[1].Claims))
	for _, c := range dcql.Credentials[1].Claims {
		second = append(second, strings.Join(c.Path, "."))
	}

	if strings.Join(second, ",") != "course,email" {
		t.Fatalf("second credential asks %v", second)
	}
}

func TestRespondVerifiesEveryCredentialAndKeepsThemApart(t *testing.T) {
	w := newWallet(t)
	v, sess := w.verifier(employeeAndCertificate)

	got, err := v.Respond(context.Background(), "req-1", form(map[string]string{
		"credential": w.present(
			"Employee",
			map[string]any{"email": "Jerry@example.com", "department": "burgerzaken", "roles": []any{"laadpalen-aanvrager"}},
			sess.nonce,
			sess.responseURI,
		),
		"credential_2": w.present(
			"Certificate",
			map[string]any{"email": "jerry@example.com", "course": "laadpalen-management"},
			sess.nonce,
			sess.responseURI,
		),
	}))
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Credentials) != 2 || got.Credentials[0].Type != "Employee" || got.Credentials[0].Claims["department"] != "burgerzaken" ||
		got.Credentials[1].Type != "Certificate" || got.Credentials[1].Claims["course"] != "laadpalen-management" ||
		got.Credentials[1].Issuer != w.did ||
		got.Subject.ID != "Jerry@example.com" {
		t.Fatalf("presented %+v", got)
	}
}

func TestRespondRefusesAnAnswerThatIsNotAllOfWhatWasAsked(t *testing.T) {
	w := newWallet(t)

	cases := map[string]func(sess session) map[string]string{
		"the certificate is missing": func(sess session) map[string]string {
			return map[string]string{
				"credential": w.present(
					"Employee",
					map[string]any{"email": "j@example.com", "department": "d", "roles": []any{"r"}},
					sess.nonce,
					sess.responseURI,
				),
			}
		},
		"another person's certificate": func(sess session) map[string]string {
			return map[string]string{
				"credential": w.present(
					"Employee",
					map[string]any{"email": "j@example.com", "department": "d", "roles": []any{"r"}},
					sess.nonce,
					sess.responseURI,
				),
				"credential_2": w.present("Certificate", map[string]any{"email": "other@example.com", "course": "c"}, sess.nonce, sess.responseURI),
			}
		},
		"the wrong type for the second query": func(sess session) map[string]string {
			return map[string]string{
				"credential": w.present(
					"Employee",
					map[string]any{"email": "j@example.com", "department": "d", "roles": []any{"r"}},
					sess.nonce,
					sess.responseURI,
				),
				"credential_2": w.present("Employee", map[string]any{"email": "j@example.com", "course": "c"}, sess.nonce, sess.responseURI),
			}
		},
		"a claim of the certificate was not disclosed": func(sess session) map[string]string {
			return map[string]string{
				"credential": w.present(
					"Employee",
					map[string]any{"email": "j@example.com", "department": "d", "roles": []any{"r"}},
					sess.nonce,
					sess.responseURI,
				),
				"credential_2": w.present("Certificate", map[string]any{"email": "j@example.com"}, sess.nonce, sess.responseURI),
			}
		},
	}
	for name, make := range cases {
		v, sess := w.verifier(employeeAndCertificate)

		_, err := v.Respond(context.Background(), "req-1", form(make(sess)))
		if !errors.Is(err, ErrInvalidPresentation) {
			t.Errorf("%s: error %v", name, err)
		}
	}
}

func TestNewRequestNeedsACredential(t *testing.T) {
	v := New(Options{PublicURL: "http://backend.test", Keys: sdjwt.DIDKey{}})
	if _, _, err := v.NewRequest(Request{Resource: "r", PolicyID: "p"}); err == nil {
		t.Fatal("a request without credentials was accepted")
	}
}

// asked records what the verifier asked the status checker.
type asked struct {
	issuer string
	ref    *sdjwt.StatusReference
	err    error
}

func (a *asked) Check(_ context.Context, issuer string, ref *sdjwt.StatusReference) error {
	a.issuer, a.ref = issuer, ref
	return a.err
}

func TestRespondAsksAboutTheCredentialsStatusAndRefusesWhatIsRevoked(t *testing.T) {
	w := newWallet(t)

	one := Request{Resource: "r", PolicyID: "p", Credentials: []CredentialQuery{{CredentialType: "Employee", Claims: []string{"department"}}}}
	for name, tc := range map[string]struct {
		err  error
		want error
	}{
		"in good standing": {nil, nil},
		"revoked":          {status.ErrRevoked, status.ErrRevoked},
		"no reference":     {status.ErrNoStatus, status.ErrNoStatus},
		"unavailable":      {status.ErrUnavailable, status.ErrUnavailable},
	} {
		v, sess := w.verifier(one)
		check := &asked{err: tc.err}
		v.status = check

		_, err := v.Respond(context.Background(), "req-1", form(map[string]string{
			"credential": w.present("Employee", map[string]any{"email": "j@example.com", "department": "d"}, sess.nonce, sess.responseURI),
		}))
		if check.issuer != w.did {
			t.Errorf("%s: asked about issuer %q", name, check.issuer)
		}

		if tc.want == nil {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}

			continue
		}

		var rejected *RejectedError
		if !errors.Is(err, tc.want) || !errors.Is(err, ErrInvalidPresentation) || !errors.As(err, &rejected) {
			t.Errorf("%s: error %v", name, err)
		}
	}
}

func TestRespondRefusesEverythingWithoutAStatusChecker(t *testing.T) {
	w := newWallet(t)
	one := Request{Resource: "r", PolicyID: "p", Credentials: []CredentialQuery{{CredentialType: "Employee", Claims: []string{"department"}}}}
	v, sess := w.verifier(one)
	v.status = nil

	_, err := v.Respond(context.Background(), "req-1", form(map[string]string{
		"credential": w.present("Employee", map[string]any{"email": "j@example.com", "department": "d"}, sess.nonce, sess.responseURI),
	}))
	if !errors.Is(err, ErrInvalidPresentation) {
		t.Fatalf("error %v", err)
	}
}
