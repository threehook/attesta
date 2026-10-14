package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/httpapi"
	"attesta/issuer/internal/service"
)

type fake struct {
	diploma            service.DiplomaInput
	employee           service.EmployeeInput
	mailed             []string
	recordErr, mailErr error
}

func (f *fake) RecordDiploma(_ context.Context, in service.DiplomaInput) (entitlement.Entitlement, error) {
	f.diploma = in
	return entitlement.Entitlement{ID: "id-1", Email: strings.ToLower(in.Email)}, f.recordErr
}

func (f *fake) RecordEmployee(_ context.Context, in service.EmployeeInput) (entitlement.Entitlement, error) {
	f.employee = in
	return entitlement.Entitlement{ID: "id-2", Email: strings.ToLower(in.Email)}, f.recordErr
}

func (f *fake) MailQRCode(_ context.Context, e entitlement.Entitlement) error {
	f.mailed = append(f.mailed, e.ID)
	return f.mailErr
}

func setup(f *fake) http.Handler {
	protocol := http.NewServeMux()
	protocol.HandleFunc("GET /oid4vci/ping", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("pong")) })
	return httpapi.Handler(httpapi.Config{IssuerName: "Gemeente Utrecht", DID: "did:key:z6Mk", Protocol: protocol}, f)
}

func call(h http.Handler, method, path, body string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	out := map[string]any{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestTellsThePagesWhichIssuerItIsAndWhatTheFormsAsk(t *testing.T) {
	code, body := call(setup(&fake{}), "GET", "/info", "")

	descriptions, _ := body["descriptions"].(map[string]any)
	departments, _ := body["departments"].([]any)
	if code != 200 || body["issuerName"] != "Gemeente Utrecht" || body["did"] != "did:key:z6Mk" || len(departments) != 2 || descriptions["Employee"] == "" || descriptions["Diploma"] == "" {
		t.Fatalf("%d %v", code, body)
	}
}

func TestRecordsAnEntitlementAndMailsItsQRCode(t *testing.T) {
	f := &fake{}
	code, body := call(setup(f), "POST", "/entitlements/employee",
		`{"name":"Jerry","email":"Jerry@Example.com","department":"burgerzaken","organisation":"Utrecht","diploma":"Laadpalen","diplomaValidUntil":"31-12-2099","description":"x"}`)

	if code != 200 || body["entitlementId"] != "id-2" || body["mailedTo"] != "jerry@example.com" || len(f.mailed) != 1 {
		t.Fatalf("%d %v mailed %v", code, body, f.mailed)
	}
	if f.employee.Department != "burgerzaken" || f.employee.DiplomaValidUntil != "31-12-2099" || f.employee.Description != "x" {
		t.Fatalf("input %+v", f.employee)
	}
}

func TestRecordsADiplomaToo(t *testing.T) {
	f := &fake{}
	code, body := call(setup(f), "POST", "/entitlements/diploma", `{"name":"Jerry","email":"j@example.com","degree":"Wiskunde","university":"UU"}`)

	if code != 200 || body["entitlementId"] != "id-1" || f.diploma.University != "UU" {
		t.Fatalf("%d %v %+v", code, body, f.diploma)
	}
}

func TestSaysWhyARequestWasRefusedAndMailsNothing(t *testing.T) {
	f := &fake{recordErr: service.InvalidError{Message: "email is not an email address"}}
	code, body := call(setup(f), "POST", "/entitlements/diploma", `{"email":"x"}`)

	if code != 400 || body["error"] != "email is not an email address" || len(f.mailed) != 0 {
		t.Fatalf("%d %v mailed %v", code, body, f.mailed)
	}
}

func TestRefusesABodyThatIsNotJSON(t *testing.T) {
	f := &fake{}
	for _, path := range []string{"/entitlements/diploma", "/entitlements/employee"} {
		if code, _ := call(setup(f), "POST", path, "name=x"); code != 400 {
			t.Errorf("%s: %d", path, code)
		}
	}
	if len(f.mailed) != 0 {
		t.Fatal("mailed")
	}
}

func TestKeepsTheEntitlementAndSaysSoWhenTheMailCannotBeSent(t *testing.T) {
	f := &fake{mailErr: errors.New("connection refused")}
	code, body := call(setup(f), "POST", "/entitlements/diploma", `{"name":"J","email":"j@example.com","degree":"d","university":"u"}`)

	if code != 502 || body["entitlementId"] != "id-1" || !strings.Contains(body["error"].(string), "vastgelegd") {
		t.Fatalf("%d %v", code, body)
	}
}

func TestAStorageErrorIsNotShownToThePages(t *testing.T) {
	code, body := call(setup(&fake{recordErr: errors.New("password authentication failed")}), "POST", "/entitlements/diploma", `{}`)

	if code != 500 || strings.Contains(body["error"].(string), "password") {
		t.Fatalf("%d %v", code, body)
	}
}

func TestServesTheWalletEndpointsNextToTheAPI(t *testing.T) {
	rec := httptest.NewRecorder()
	setup(&fake{}).ServeHTTP(rec, httptest.NewRequest("GET", "/oid4vci/ping", nil))

	if rec.Body.String() != "pong" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}

func TestHasNoEndpointThatMakesALink(t *testing.T) {
	for _, path := range []string{"/offers/diploma", "/offers/employee", "/entitlements/diploma/offer", "/login"} {
		if code, _ := call(setup(&fake{}), "POST", path, "{}"); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d", path, code)
		}
	}
}

func TestOnlyStaffUseTheFormsEndpointsButWalletsAndProbesNeedNoSignIn(t *testing.T) {
	protocol := http.NewServeMux()
	protocol.HandleFunc("GET /oid4vci/ping", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("pong")) })
	f := &fake{}
	h := httpapi.Handler(httpapi.Config{
		Protocol: protocol,
		Protect: func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
		},
	}, f)

	for _, c := range []struct{ method, path string }{{"GET", "/info"}, {"POST", "/entitlements/employee"}, {"POST", "/entitlements/diploma"}} {
		if code, _ := call(h, c.method, c.path, "{}"); code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", c.method, c.path, code)
		}
	}
	for _, path := range []string{"/oid4vci/ping", "/healthz"} {
		if code, _ := call(h, "GET", path, ""); code >= 400 {
			t.Errorf("%s: %d", path, code)
		}
	}
}
