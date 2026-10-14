package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/httpapi"
	"attesta/issuer/internal/service"
)

type fake struct {
	diploma            service.DiplomaInput
	diplomaType        string
	rolesErr           error
	employee           service.EmployeeInput
	mailed             []string
	recordErr, mailErr error
}

func (f *fake) RecordDiploma(_ context.Context, d credential.Diploma, in service.DiplomaInput) (entitlement.Entitlement, error) {
	f.diploma, f.diplomaType = in, d.Type
	return entitlement.Entitlement{ID: "id-1", Email: strings.ToLower(in.Email)}, f.recordErr
}

func (f *fake) RecordEmployee(_ context.Context, in service.EmployeeInput) (entitlement.Entitlement, error) {
	f.employee = in
	return entitlement.Entitlement{ID: "id-2", Email: strings.ToLower(in.Email)}, f.recordErr
}

func (f *fake) Roles(context.Context) ([]string, error) {
	return []string{"laadpalen-aanvrager", "reader"}, f.rolesErr
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

func TestTellsThePagesWhichIssuerItIsAndWhatTheFormsOffer(t *testing.T) {
	code, body := call(setup(&fake{}), "GET", "/info", "")

	departments, _ := body["departments"].([]any)
	roles, _ := body["roles"].([]any)
	diplomas, _ := body["diplomas"].([]any)
	if code != 200 || body["issuerName"] != "Gemeente Utrecht" || body["did"] != "did:key:z6Mk" || len(departments) != 2 || len(roles) != 2 || len(diplomas) != 1 {
		t.Fatalf("%d %v", code, body)
	}
	diploma, _ := diplomas[0].(map[string]any)
	if diploma["path"] != "laadpalen-management" || diploma["title"] != "Diploma Laadpalen Management" || diploma["description"] == "" {
		t.Fatalf("diploma %v", diploma)
	}
}

func TestRecordsAnEntitlementAndMailsItsQRCode(t *testing.T) {
	f := &fake{}
	code, body := call(setup(f), "POST", "/entitlements/employee",
		`{"name":"Jerry","email":"Jerry@Example.com","department":"burgerzaken","organisation":"Utrecht","roles":["reader","laadpalen-aanvrager"]}`)

	if code != 200 || body["entitlementId"] != "id-2" || body["mailedTo"] != "jerry@example.com" || len(f.mailed) != 1 {
		t.Fatalf("%d %v mailed %v", code, body, f.mailed)
	}
	if f.employee.Department != "burgerzaken" || strings.Join(f.employee.Roles, ",") != "reader,laadpalen-aanvrager" {
		t.Fatalf("input %+v", f.employee)
	}
}

func TestRecordsADiplomaToo(t *testing.T) {
	f := &fake{}
	code, body := call(setup(f), "POST", "/entitlements/diploma/laadpalen-management", `{"name":"Jerry","email":"j@example.com","validUntil":"31-12-2099"}`)

	if code != 200 || body["entitlementId"] != "id-1" || f.diplomaType != "Diploma Laadpalen Management" || f.diploma.ValidUntil != "31-12-2099" {
		t.Fatalf("%d %v %+v", code, body, f.diploma)
	}
}

func TestSaysWhyARequestWasRefusedAndMailsNothing(t *testing.T) {
	f := &fake{recordErr: service.InvalidError{Message: "email is not an email address"}}
	code, body := call(setup(f), "POST", "/entitlements/diploma/laadpalen-management", `{"email":"x"}`)

	if code != 400 || body["error"] != "email is not an email address" || len(f.mailed) != 0 {
		t.Fatalf("%d %v mailed %v", code, body, f.mailed)
	}
}

func TestRefusesABodyThatIsNotJSON(t *testing.T) {
	f := &fake{}
	for _, path := range []string{"/entitlements/diploma/laadpalen-management", "/entitlements/employee"} {
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
	code, body := call(setup(f), "POST", "/entitlements/diploma/laadpalen-management", `{"name":"J","email":"j@example.com","validUntil":"31-12-2099"}`)

	if code != 502 || body["entitlementId"] != "id-1" || !strings.Contains(body["error"].(string), "vastgelegd") {
		t.Fatalf("%d %v", code, body)
	}
}

func TestAStorageErrorIsNotShownToThePages(t *testing.T) {
	code, body := call(setup(&fake{recordErr: errors.New("password authentication failed")}), "POST", "/entitlements/diploma/laadpalen-management", `{}`)

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
	for _, path := range []string{"/offers/diploma", "/offers/employee", "/entitlements/diploma/laadpalen-management/offer", "/login"} {
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

	for _, c := range []struct{ method, path string }{{"GET", "/info"}, {"POST", "/entitlements/employee"}, {"POST", "/entitlements/diploma/laadpalen-management"}} {
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

func TestSaysConflictAndMailsNothingWhenThePersonAlreadyHasTheCredential(t *testing.T) {
	f := &fake{recordErr: service.AlreadyIssuedError{IssuedAt: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)}}
	code, body := call(setup(f), "POST", "/entitlements/employee", `{}`)

	if code != 409 || body["error"] != "deze persoon heeft al een geldig credential van dit type (uitgegeven op 14-10-2026); dat moet eerst worden ingetrokken" || len(f.mailed) != 0 {
		t.Fatalf("%d %v mailed %v", code, body, f.mailed)
	}
}

func TestKnowsOnlyTheDiplomasItOffers(t *testing.T) {
	f := &fake{}
	if code, _ := call(setup(f), "POST", "/entitlements/diploma/koeien-melken", `{"name":"J","email":"j@example.com","validUntil":"31-12-2099"}`); code != 404 {
		t.Fatalf("an unknown diploma: %d", code)
	}
	if f.diplomaType != "" {
		t.Fatal("an unknown diploma was recorded")
	}
}

func TestSaysSoWhenTheRolesCannotBeRead(t *testing.T) {
	if code, _ := call(setup(&fake{rolesErr: errors.New("connection refused")}), "GET", "/info", ""); code != http.StatusInternalServerError {
		t.Fatalf("status %d", code)
	}
}
