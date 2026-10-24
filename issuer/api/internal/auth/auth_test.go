package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// loginServer is a stand-in OpenID Connect server: metadata, keys, and tokens signed by them.
type loginServer struct {
	*httptest.Server

	key *rsa.PrivateKey
}

func newLoginServer(t *testing.T) *loginServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	s := &loginServer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": "https://login.example/realms/r", "jwks_uri": s.URL + "/keys", "authorization_endpoint": "https://login.example/auth",
			"token_endpoint": s.URL + "/token", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "k", "alg": "RS256", "use": "sig", "n": b64(key.N.Bytes()), "e": "AQAB",
		}}})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)

	return s
}

func (s *loginServer) token(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()

	claims := jwt.MapClaims{
		"iss": "https://login.example/realms/r", "sub": "u", "azp": "issuer-gui", "exp": time.Now().Add(time.Minute).Unix(),
		"iat": time.Now().Unix(), "realm_access": map[string]any{"roles": []string{"issuer-staff"}},
	}
	if mutate != nil {
		mutate(claims)
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k"

	raw, err := tok.SignedString(s.key)
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func call(h http.Handler, bearer string) int {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/info", nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	return w.Code
}

func TestProtect(t *testing.T) {
	login := newLoginServer(t)
	v := New(Options{Issuer: "https://login.example/realms/r", DiscoveryURL: login.URL, ClientID: "issuer-gui", Role: "issuer-staff"})
	h := v.Protect(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	forged := jwt.NewWithClaims(
		jwt.SigningMethodRS256,
		jwt.MapClaims{
			"iss":          "https://login.example/realms/r",
			"azp":          "issuer-gui",
			"exp":          time.Now().Add(time.Minute).Unix(),
			"realm_access": map[string]any{"roles": []string{"issuer-staff"}},
		},
	)
	forged.Header["kid"] = "k"
	forgedRaw, _ := forged.SignedString(other)

	cases := []struct {
		name   string
		bearer string
		want   int
	}{
		{"staff", login.token(t, nil), 200},
		{"flat roles claim", login.token(t, func(c jwt.MapClaims) { delete(c, "realm_access"); c["roles"] = []string{"issuer-staff"} }), 200},
		{"no token", "", 401},
		{"garbage", "abc", 401},
		{"expired", login.token(t, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }), 401},
		{"signed by someone else", forgedRaw, 401},
		{"other issuer", login.token(t, func(c jwt.MapClaims) { c["iss"] = "https://evil.example" }), 401},
		{"no role", login.token(t, func(c jwt.MapClaims) { c["realm_access"] = map[string]any{"roles": []string{"other"}} }), 403},
		{"other application", login.token(t, func(c jwt.MapClaims) { c["azp"] = "something-else" }), 403},
	}
	for _, c := range cases {
		if got := call(h, c.bearer); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestLoginServerDown(t *testing.T) {
	login := newLoginServer(t)
	url := login.URL
	login.Close()

	v := New(Options{Issuer: "https://login.example/realms/r", DiscoveryURL: url, ClientID: "issuer-gui", Role: "issuer-staff"})

	h := v.Protect(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	if got := call(h, "abc"); got != http.StatusServiceUnavailable {
		t.Fatalf("got %d", got)
	}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
