package oid4vci_test

import (
	"attesta/issuer/internal/credtype"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/identity"
	"attesta/issuer/internal/oid4vci"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	sdjwt "github.com/threehook/sdjwt-go"
)

var start = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

const (
	lifetime = 24 * time.Hour
	cooldown = 10 * time.Minute
)

// forgetful cannot record that a credential was made, as if the issuer had stopped just then.
type forgetful struct {
	*entitlement.Store

	forget atomic.Bool
}

func (f *forgetful) MarkIssued(ctx context.Context, id string) (bool, error) {
	if f.forget.Load() {
		return false, nil
	}

	return f.Store.MarkIssued(ctx, id)
}

type env struct {
	t        *testing.T
	server   *oid4vci.Server
	http     *httptest.Server
	store    *forgetful
	sessions *oid4vci.Sessions
	issuer   identity.Identity
	now      time.Time
	renewed  []string
	cfg      oid4vci.Config
	secret   []byte
	types    *credtype.Store
}

func newEnv(t *testing.T, lease time.Duration) *env {
	t.Helper()

	pool, err := postgres.Open(context.Background(), postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)
	e := &env{t: t, now: start, issuer: identity.FromSeed("test"), secret: []byte("secret")}
	e.types = credtype.NewStore(pool)
	e.store = &forgetful{Store: entitlement.NewStore(pool, lease)}
	e.sessions = oid4vci.NewSessions(pool, e.secret)
	e.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { e.server.Handler().ServeHTTP(w, r) }))
	t.Cleanup(e.http.Close)
	e.cfg = oid4vci.Config{
		PublicURL: e.http.URL, StatusURI: e.http.URL + "/status/1", IssuerName: "Gemeente Vlierdam", Identity: e.issuer, Secret: e.secret,
		OfferLifetime: lifetime, RenewalCooldown: cooldown, Now: func() time.Time { return e.now },
	}
	e.server = oid4vci.New(e.cfg, e.sessions, e.store, e.types, func(ctx context.Context, ent entitlement.Entitlement) error {
		e.renewed = append(e.renewed, ent.ID)
		_, err := e.server.Offer(ctx, ent)

		return err
	})

	return e
}

func (e *env) base() string { return e.http.URL + "/oid4vci" }

var employeeClaims = map[string]string{
	"name": "Jerry Smith", "email": "jerry@example.com", "department": "burgerzaken", "organisation": "Gemeente Vlierdam",
	"roles": "laadpalen-aanvrager",
}

func (e *env) entitle(edit func(*entitlement.New)) entitlement.Entitlement {
	e.t.Helper()

	n := entitlement.New{Subject: entitlement.PersonOf(uuid.NewString() + "@example.com"), Type: "Employee", Claims: employeeClaims}
	if edit != nil {
		edit(&n)
	}

	ent, err := e.store.Create(context.Background(), n)
	if err != nil {
		e.t.Fatal(err)
	}

	return ent
}

func (e *env) offer(ent entitlement.Entitlement) string {
	e.t.Helper()

	link, err := e.server.Offer(context.Background(), ent)
	if err != nil {
		e.t.Fatal(err)
	}

	return link
}

// A wallet, as far as the protocol goes.

type response struct {
	status int
	body   map[string]any
}

func (r response) errorCode() string { s, _ := r.body["error"].(string); return s }

func do(t *testing.T, req *http.Request) response {
	t.Helper()

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	body := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&body) // not every response is JSON

	return response{res.StatusCode, body}
}

func get(t *testing.T, u string) response {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)

	return do(t, req)
}

func (e *env) fetchOffer(link string) response {
	e.t.Helper()

	u, err := url.Parse(link)
	if err != nil || u.Scheme != "openid-credential-offer" {
		e.t.Fatalf("link %q", link)
	}

	return get(e.t, u.Query().Get("credential_offer_uri"))
}

func (e *env) token(code string) response {
	e.t.Helper()
	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		e.base()+"/token",
		strings.NewReader(
			url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:pre-authorized_code"}, "pre-authorized_code": {code}}.Encode(),
		),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return do(e.t, req)
}

func (e *env) nonce() string {
	e.t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, e.base()+"/nonce", nil)
	n, _ := do(e.t, req).body["c_nonce"].(string)

	return n
}

type holder struct {
	signer jwt.SigningMethod
	key    any
	jwk    map[string]any
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func newEd25519Holder() holder {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	return holder{jwt.SigningMethodEdDSA, key, map[string]any{"kty": "OKP", "crv": "Ed25519", "x": b64(pub)}}
}

func newP256Holder() holder {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	return holder{
		jwt.SigningMethodES256,
		key,
		map[string]any{"kty": "EC", "crv": "P-256", "x": b64(key.X.FillBytes(make([]byte, 32))), "y": b64(key.Y.FillBytes(make([]byte, 32)))},
	}
}

type proofOptions struct {
	aud, nonce, typ string
	iat             time.Time
	jwk             any
}

func (h holder) proof(t *testing.T, o proofOptions) string {
	t.Helper()

	tok := jwt.NewWithClaims(h.signer, jwt.MapClaims{"aud": o.aud, "nonce": o.nonce, "iat": o.iat.Unix()})

	tok.Header["typ"] = o.typ
	if o.jwk != nil {
		tok.Header["jwk"] = o.jwk
	}

	s, err := tok.SignedString(h.key)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func (e *env) goodProof(h holder) string {
	return h.proof(e.t, proofOptions{aud: e.base(), nonce: e.nonce(), typ: "openid4vci-proof+jwt", iat: e.now, jwk: h.jwk})
}

func (e *env) credential(token string, body any) response {
	e.t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, e.base()+"/credential", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return do(e.t, req)
}

func request(config string, proofs ...string) map[string]any {
	return map[string]any{"credential_configuration_id": config, "proofs": map[string]any{"jwt": proofs}}
}

// code and token take a wallet from the offer to the access token.
func (e *env) codeOf(link string) string {
	e.t.Helper()

	offer := e.fetchOffer(link)
	if offer.status != 200 {
		e.t.Fatalf("offer: %d %v", offer.status, offer.body)
	}

	grants := object(object(offer.body["grants"])["urn:ietf:params:oauth:grant-type:pre-authorized_code"])

	return text(grants["pre-authorized_code"])
}

func (e *env) accessToken(link string) string {
	e.t.Helper()

	res := e.token(e.codeOf(link))
	if res.status != 200 {
		e.t.Fatalf("token: %d %v", res.status, res.body)
	}

	return text(res.body["access_token"])
}

func issuedCredential(t *testing.T, res response) string {
	t.Helper()

	if res.status != 200 {
		t.Fatalf("credential: %d %v", res.status, res.body)
	}

	credentials, _ := res.body["credentials"].([]any)

	return text(object(credentials[0])["credential"])
}

// show has the holder present the credential with only the given claim disclosed, and verifies it as the backend would.
func (e *env) show(t *testing.T, credential string, h holder, disclose string) (*sdjwt.Verified, error) {
	t.Helper()

	parts := strings.Split(strings.TrimSuffix(credential, "~"), "~")
	shown := parts[0] + "~"

	var shownSb262 strings.Builder

	for _, d := range parts[1:] {
		raw, _ := base64.RawURLEncoding.DecodeString(d)
		if strings.Contains(string(raw), `"`+disclose+`"`) {
			shownSb262.WriteString(d + "~")
		}
	}

	shown += shownSb262.String()
	sum := sha256.Sum256([]byte(shown))
	kb := jwt.NewWithClaims(h.signer, jwt.MapClaims{"aud": "a", "nonce": "n", "iat": e.now.Unix(), "sd_hash": b64(sum[:])})
	kb.Header["typ"] = "kb+jwt"

	signed, err := kb.SignedString(h.key)
	if err != nil {
		t.Fatal(err)
	}

	return sdjwt.Verify(shown+signed, sdjwt.Options{Keys: sdjwt.DIDKey{}, Audience: "a", Nonce: "n", Now: func() time.Time { return e.now }})
}

// receive is the whole way of a wallet that follows the QR code.
func (e *env) receive(link string, h holder) string {
	e.t.Helper()
	token := e.accessToken(link)

	return issuedCredential(e.t, e.credential(token, request("Employee", e.goodProof(h))))
}

func (e *env) status(id string) entitlement.Status {
	e.t.Helper()

	ent, err := e.store.Find(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}

	return ent.Status
}

func TestServesTheMetadataWalletsAskFor(t *testing.T) {
	e := newEnv(t, 0)
	for _, path := range []string{"/oid4vci/.well-known/openid-credential-issuer", "/.well-known/openid-credential-issuer/oid4vci"} {
		res := get(t, e.http.URL+path)
		configs, _ := res.body["credential_configurations_supported"].(map[string]any)

		employee, _ := configs["Employee"].(map[string]any)
		if res.status != 200 || res.body["credential_issuer"] != e.base() || res.body["credential_endpoint"] != e.base()+"/credential" ||
			res.body["nonce_endpoint"] != e.base()+"/nonce" || employee["format"] != "dc+sd-jwt" || employee["vct"] != "Employee" ||
			configs["Diploma Laadpalen Management"] == nil {
			t.Fatalf("%s: %d %v", path, res.status, res.body)
		}
	}

	for _, path := range []string{"/oid4vci/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/oid4vci"} {
		res := get(t, e.http.URL+path)
		if res.status != 200 || res.body["token_endpoint"] != e.base()+"/token" ||
			res.body["pre-authorized_grant_anonymous_access_supported"] != true {
			t.Fatalf("%s: %d %v", path, res.status, res.body)
		}
	}
}

func TestGivesTheHolderTheirCredentialAndMarksTheEntitlementIssued(t *testing.T) {
	e := newEnv(t, 0)
	ent := e.entitle(nil)
	h := newEd25519Holder()

	credential := e.receive(e.offer(ent), h)

	v, err := e.show(t, credential, h, "email")
	if err != nil {
		t.Fatal(err)
	}

	if v.Issuer != e.issuer.DID || v.Type != "Employee" || v.Claims["email"] != "jerry@example.com" ||
		v.Claims["issuer_name"] != "Gemeente Vlierdam" {
		t.Fatalf("verified %+v", v)
	}

	if _, shown := v.Claims["department"]; shown {
		t.Fatal("department was shown")
	}

	if e.status(ent.ID) != entitlement.Issued {
		t.Fatalf("entitlement is %s", e.status(ent.ID))
	}
}

func TestBindsTheCredentialToAP256KeyToo(t *testing.T) {
	e := newEnv(t, 0)
	h := newP256Holder()

	credential := e.receive(e.offer(e.entitle(nil)), h)
	if _, err := e.show(t, credential, h, "email"); err != nil {
		t.Fatal(err)
	}
}

func TestWorksOnce(t *testing.T) {
	e := newEnv(t, 0)
	link := e.offer(e.entitle(nil))
	e.receive(link, newEd25519Holder())

	if res := e.fetchOffer(link); res.status != http.StatusGone || res.errorCode() != "qr_gone" {
		t.Fatalf("second fetch: %d %v", res.status, res.body)
	}
}

func TestACodeBuysOneAccessTokenOnly(t *testing.T) {
	e := newEnv(t, 0)
	code := e.codeOf(e.offer(e.entitle(nil)))

	if first := e.token(code); first.status != 200 {
		t.Fatalf("first: %d %v", first.status, first.body)
	}

	if second := e.token(code); second.status != 400 || second.errorCode() != "invalid_grant" {
		t.Fatalf("second: %d %v", second.status, second.body)
	}

	if bad := e.token("made-up"); bad.errorCode() != "invalid_grant" {
		t.Fatalf("made up: %v", bad.body)
	}
}

func TestOnlyThePreAuthorizedGrantIsOffered(t *testing.T) {
	e := newEnv(t, 0)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, e.base()+"/token", strings.NewReader("grant_type=authorization_code&code=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if res := do(t, req); res.errorCode() != "unsupported_grant_type" {
		t.Fatalf("%d %v", res.status, res.body)
	}
}

func TestIssuesNothingForAnEntitlementThatWasRevoked(t *testing.T) {
	e := newEnv(t, 0)
	ent := e.entitle(nil)

	link := e.offer(ent)
	if revoked, err := e.store.Revoke(context.Background(), ent.ID); err != nil || !revoked {
		t.Fatal(revoked, err)
	}

	token := e.accessToken(link)

	res := e.credential(token, request("Employee", e.goodProof(newEd25519Holder())))
	if res.status != 400 || res.errorCode() != "credential_request_denied" {
		t.Fatalf("%d %v", res.status, res.body)
	}
}

func TestIssuesNothingForAnEntitlementPastItsEnd(t *testing.T) {
	e := newEnv(t, 0)
	end := e.now.Add(time.Hour)
	ent := e.entitle(func(n *entitlement.New) { n.ValidUntil = &end })
	link := e.offer(ent)
	// The entitlement ends after the offer is made; the database no longer lists it only if its clock passed the end, so the issuer's own clock
	// decides.
	e.now = e.now.Add(2 * time.Hour)

	token := e.accessToken(link)

	res := e.credential(token, request("Employee", e.goodProof(newEd25519Holder())))
	if res.errorCode() != "credential_request_denied" {
		t.Fatalf("%d %v", res.status, res.body)
	}
}

func TestRefusesWhatIsNotAProperRequest(t *testing.T) {
	h := newEd25519Holder()
	e := newEnv(t, 0)
	good := func() proofOptions {
		return proofOptions{aud: e.base(), nonce: e.nonce(), typ: "openid4vci-proof+jwt", iat: e.now, jwk: h.jwk}
	}
	withPrivate := map[string]any{"kty": "OKP", "crv": "Ed25519", "x": h.jwk["x"], "d": "nWGxne_9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A"}

	cases := map[string]struct {
		edit func(*proofOptions)
		want string
	}{
		"another audience":  {func(o *proofOptions) { o.aud = "https://elsewhere.example" }, "invalid_proof"},
		"another typ":       {func(o *proofOptions) { o.typ = "JWT" }, "invalid_proof"},
		"no key":            {func(o *proofOptions) { o.jwk = nil }, "invalid_proof"},
		"a private key":     {func(o *proofOptions) { o.jwk = withPrivate }, "invalid_proof"},
		"an old proof":      {func(o *proofOptions) { o.iat = e.now.Add(-time.Hour) }, "invalid_proof"},
		"a made-up nonce":   {func(o *proofOptions) { o.nonce = "made-up" }, "invalid_nonce"},
		"no nonce":          {func(o *proofOptions) { o.nonce = "" }, "invalid_nonce"},
		"another key's sig": {func(o *proofOptions) { o.jwk = newEd25519Holder().jwk }, "invalid_proof"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			token := e.accessToken(e.offer(e.entitle(nil)))
			o := good()
			c.edit(&o)

			res := e.credential(token, request("Employee", h.proof(t, o)))
			if res.status != 400 || res.errorCode() != c.want {
				t.Fatalf("%d %v, want %s", res.status, res.body, c.want)
			}
			// A refused request must not use up the entitlement.
			if res := e.credential(token, request("Employee", e.goodProof(h))); res.status != 200 {
				t.Fatalf("after the refusal: %d %v", res.status, res.body)
			}
		})
	}
}

func TestRefusesTheWrongRequests(t *testing.T) {
	e := newEnv(t, 0)
	h := newEd25519Holder()
	token := e.accessToken(e.offer(e.entitle(nil)))

	if res := e.credential("", request("Employee", e.goodProof(h))); res.status != 401 {
		t.Errorf("no token: %d", res.status)
	}

	if res := e.credential("made-up", request("Employee", e.goodProof(h))); res.status != 401 {
		t.Errorf("made-up token: %d", res.status)
	}

	if res := e.credential(token, request("Diploma Laadpalen Management", e.goodProof(h))); res.errorCode() != "invalid_credential_request" {
		t.Errorf("another credential: %d %v", res.status, res.body)
	}

	if res := e.credential(token, request("Employee", e.goodProof(h), e.goodProof(h))); res.errorCode() != "invalid_proof" {
		t.Errorf("two proofs: %d %v", res.status, res.body)
	}

	if res := e.credential(token, request("Employee")); res.errorCode() != "invalid_proof" {
		t.Errorf("no proof: %d %v", res.status, res.body)
	}
}

func TestAnAccessTokenExpires(t *testing.T) {
	e := newEnv(t, 0)
	token := e.accessToken(e.offer(e.entitle(nil)))
	e.now = e.now.Add(6 * time.Minute)

	if res := e.credential(token, request("Employee", e.goodProof(newEd25519Holder()))); res.status != 401 {
		t.Fatalf("%d %v", res.status, res.body)
	}
}

func TestAnAccessTokenWorksOnce(t *testing.T) {
	e := newEnv(t, 0)
	h := newEd25519Holder()
	token := e.accessToken(e.offer(e.entitle(nil)))
	issuedCredential(t, e.credential(token, request("Employee", e.goodProof(h))))

	if res := e.credential(token, request("Employee", e.goodProof(h))); res.status != 401 {
		t.Fatalf("%d %v", res.status, res.body)
	}
}

func TestKnowsNothingOfAQRCodeThatNeverExisted(t *testing.T) {
	e := newEnv(t, 0)
	if res := get(t, e.base()+"/offers/made-up"); res.status != 404 {
		t.Fatalf("%d %v", res.status, res.body)
	}
}

func TestGivesTheHolderTheirEntitlementAgainAfterAnAttemptThatEndedNoCredential(t *testing.T) {
	e := newEnv(t, 300*time.Millisecond)
	ent := e.entitle(nil)
	// An attempt that took the entitlement and then broke off.
	if claimed, err := e.store.Claim(context.Background(), ent.ID); err != nil || !claimed {
		t.Fatal(claimed, err)
	}

	h := newEd25519Holder()

	token := e.accessToken(e.offer(ent))
	if res := e.credential(token, request("Employee", e.goodProof(h))); res.errorCode() != "credential_request_denied" {
		t.Fatalf("%d %v", res.status, res.body)
	}

	time.Sleep(400 * time.Millisecond)

	if _, err := e.show(t, e.receive(e.offer(ent), h), h, "email"); err != nil {
		t.Fatal(err)
	}
}

func TestAnExpiredQRCodeMakesTheIssuerSendANewOne(t *testing.T) {
	e := newEnv(t, 0)
	ent := e.entitle(nil)
	link := e.offer(ent)
	e.now = e.now.Add(lifetime + time.Minute)

	res := e.fetchOffer(link)
	if res.status != http.StatusGone || res.errorCode() != "qr_expired" || len(e.renewed) != 1 || e.renewed[0] != ent.ID {
		t.Fatalf("%d %v renewed %v", res.status, res.body, e.renewed)
	}
}

func TestDoesNotSendAgainWithinTheCooldownButDoesAfterIt(t *testing.T) {
	e := newEnv(t, 0)
	ent := e.entitle(nil)
	link := e.offer(ent)
	e.now = e.now.Add(lifetime + time.Minute)
	e.fetchOffer(link)

	e.now = e.now.Add(cooldown - time.Minute)

	again := e.fetchOffer(link)
	if again.errorCode() != "qr_expired" || len(e.renewed) != 1 {
		t.Fatalf("within the cooldown: %v renewed %d", again.body, len(e.renewed))
	}

	e.now = e.now.Add(2 * time.Minute)
	e.fetchOffer(link)

	if len(e.renewed) != 2 {
		t.Fatalf("after the cooldown: renewed %d", len(e.renewed))
	}
}

func TestSendsNothingWhenTheCredentialWasFetchedOrWithdrawn(t *testing.T) {
	e := newEnv(t, 0)
	fetched, withdrawn := e.entitle(nil), e.entitle(nil)
	fetchedLink, withdrawnLink := e.offer(fetched), e.offer(withdrawn)
	other := e.offer(fetched) // a second QR code for the same entitlement
	e.receive(other, newEd25519Holder())

	if revoked, err := e.store.Revoke(context.Background(), withdrawn.ID); err != nil || !revoked {
		t.Fatal(revoked, err)
	}

	e.now = e.now.Add(lifetime + time.Minute)

	for name, link := range map[string]string{"fetched": fetchedLink, "withdrawn": withdrawnLink} {
		if res := e.fetchOffer(link); res.status != http.StatusGone || res.errorCode() != "qr_gone" {
			t.Errorf("%s: %d %v", name, res.status, res.body)
		}
	}

	if len(e.renewed) != 0 {
		t.Fatalf("renewed %v", e.renewed)
	}
}

func TestMarksAnEntitlementIssuedAfterARestartWhenTheIssuerStoppedBeforeItCould(t *testing.T) {
	e := newEnv(t, 0)
	e.store.forget.Store(true)
	ent := e.entitle(nil)
	e.receive(e.offer(ent), newEd25519Holder())

	if e.status(ent.ID) != entitlement.Claimed {
		t.Fatalf("entitlement is %s", e.status(ent.ID))
	}

	e.store.forget.Store(false)

	restarted := oid4vci.New(e.cfg, e.sessions, e.store, e.types, nil)
	if err := restarted.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}

	if e.status(ent.ID) != entitlement.Issued {
		t.Fatalf("entitlement is %s", e.status(ent.ID))
	}
}

func TestEveryCredentialPointsToItsOwnPlaceInTheStatusList(t *testing.T) {
	e := newEnv(t, 0)
	seen := map[int]bool{}

	for range 3 {
		h := newEd25519Holder()

		v, err := e.show(t, e.receive(e.offer(e.entitle(nil)), h), h, "email")
		if err != nil {
			t.Fatal(err)
		}

		if v.Status == nil || v.Status.URI != e.http.URL+"/status/1" || seen[v.Status.Index] {
			t.Fatalf("status %+v after %v", v.Status, seen)
		}

		seen[v.Status.Index] = true
	}
}

const agentID = "spiffe://corp.test/ns/cicd/sa/agent"

func (e *env) entitleWorkload() entitlement.Entitlement {
	e.t.Helper()

	ent, err := e.store.Create(
		context.Background(),
		entitlement.New{Subject: entitlement.WorkloadOf(agentID), Type: "Workload", Claims: map[string]string{
			"workload": agentID, "owner": "platform", "environment": "staging", "expiresAt": e.now.Add(time.Hour).Format(time.RFC3339),
		}},
	)
	if err != nil {
		e.t.Fatal(err)
	}

	return ent
}

func TestGivesAWorkloadItsCredentialThroughAShortLivedOffer(t *testing.T) {
	e := newEnv(t, 0)
	ent := e.entitleWorkload()

	link, err := e.server.OfferFor(context.Background(), ent, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	h := newEd25519Holder()
	token := e.accessToken(link)
	credential := issuedCredential(t, e.credential(token, request("Workload", e.goodProof(h))))

	v, err := e.show(t, credential, h, "workload")
	if err != nil {
		t.Fatal(err)
	}

	if v.Type != "Workload" || v.Claims["workload"] != agentID || v.Claims["issuer_name"] != "Gemeente Vlierdam" {
		t.Fatalf("verified %+v", v)
	}

	if _, shown := v.Claims["owner"]; shown {
		t.Fatal("owner was shown")
	}

	if e.status(ent.ID) != entitlement.Issued {
		t.Fatalf("entitlement is %s", e.status(ent.ID))
	}
}

func TestAWorkloadOfferThatRanOutSendsNoMail(t *testing.T) {
	e := newEnv(t, 0)
	ent := e.entitleWorkload()

	link, err := e.server.OfferFor(context.Background(), ent, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	e.now = e.now.Add(3 * time.Minute)

	res := e.fetchOffer(link)
	if res.status != http.StatusGone || res.errorCode() != "offer_expired" || len(e.renewed) != 0 {
		t.Fatalf("%d %v renewed %v", res.status, res.body, e.renewed)
	}
}

func TestTheMetadataOffersTheWorkloadCredential(t *testing.T) {
	e := newEnv(t, 0)
	res := get(t, e.http.URL+"/oid4vci/.well-known/openid-credential-issuer")
	configs, _ := res.body["credential_configurations_supported"].(map[string]any)

	workload, _ := configs["Workload"].(map[string]any)
	if workload["format"] != "dc+sd-jwt" || workload["vct"] != "Workload" {
		t.Fatalf("configurations %v", configs)
	}
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
