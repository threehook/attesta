package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/credtype"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/httpapi"
	"attesta/issuer/internal/role"
	"attesta/issuer/internal/service"
)

type fake struct {
	recorded           map[string]string
	recordedType       string
	typeCalls          []string
	typeErr            error
	rolesErr           error
	employee           service.EmployeeInput
	mailed             []string
	recordErr, mailErr error
	active             []entitlement.Entitlement
	revoked            []string
	replaced           string
	replacement        service.Replacement
	revokeErr          error
	roleErr            error
	roleCalls          []string
}

func (f *fake) ListRoles(context.Context) ([]role.Role, error) {
	return []role.Role{{ID: "r1", Name: "reader", InUse: true}}, f.roleErr
}

func (f *fake) CreateRole(_ context.Context, name string) (role.Role, error) {
	f.roleCalls = append(f.roleCalls, "create "+name)
	return role.Role{ID: "r2", Name: name}, f.roleErr
}

func (f *fake) RenameRole(_ context.Context, id, name string) (role.Role, error) {
	f.roleCalls = append(f.roleCalls, "rename "+id+" "+name)
	return role.Role{ID: id, Name: name}, f.roleErr
}

func (f *fake) DeleteRole(_ context.Context, id string) error {
	f.roleCalls = append(f.roleCalls, "delete "+id)
	return f.roleErr
}

func (f *fake) Active(_ context.Context, email string) ([]entitlement.Entitlement, error) {
	if email != "jerry@example.com" {
		return nil, service.InvalidError{Message: "email is not an email address"}
	}
	return f.active, nil
}

func (f *fake) AllActive(context.Context) ([]entitlement.Entitlement, error) {
	return append([]entitlement.Entitlement{{ID: "z", Type: "Employee", Status: entitlement.Issued}}, f.active...), nil
}

func (f *fake) Revoke(_ context.Context, id string) error {
	f.revoked = append(f.revoked, id)
	return f.revokeErr
}

func (f *fake) Replace(_ context.Context, id string, r service.Replacement) (entitlement.Entitlement, error) {
	f.replaced, f.replacement = id, r
	return entitlement.Entitlement{ID: "id-3", Subject: entitlement.PersonOf(r.Values["email"])}, f.revokeErr
}

var laadpalen = credtype.Type{
	ID: "t1", InUse: true,
	Type: credential.Type{
		Type: "Diploma Laadpalen Management", Path: "laadpalen-management", Title: "Diploma Laadpalen Management", Description: "Bewijs",
		Fields: []credential.Field{{Key: "email", Label: "E-mailadres", Kind: "email", Required: true}},
	},
}

func (f *fake) RecordType(_ context.Context, d credential.Type, in map[string]string) (entitlement.Entitlement, error) {
	f.recorded, f.recordedType = in, d.Type
	return entitlement.Entitlement{ID: "id-1", Subject: entitlement.PersonOf(in["email"])}, f.recordErr
}

func (f *fake) Types(context.Context) ([]credtype.Type, error) {
	return []credtype.Type{laadpalen}, f.typeErr
}

func (f *fake) TypeByPath(_ context.Context, path string) (credtype.Type, error) {
	if path != laadpalen.Path {
		return credtype.Type{}, credtype.ErrNotFound
	}
	return laadpalen, f.typeErr
}

func (f *fake) TypeTitle(_ context.Context, typeName string) string {
	if typeName == "Employee" {
		return "Medewerker afdeling & rollen"
	}
	return typeName
}

func (f *fake) CreateType(_ context.Context, in credtype.Input) (credtype.Type, error) {
	f.typeCalls = append(f.typeCalls, "create "+in.Title+" "+fmt.Sprint(len(in.Fields)))
	return credtype.Type{ID: "t2", Type: credential.Type{Title: in.Title}}, f.typeErr
}

func (f *fake) UpdateType(_ context.Context, id string, in credtype.Input) (credtype.Type, error) {
	f.typeCalls = append(f.typeCalls, "update "+id+" "+in.Title)
	return credtype.Type{ID: id, Type: credential.Type{Title: in.Title}}, f.typeErr
}

func (f *fake) DeleteType(_ context.Context, id string) error {
	f.typeCalls = append(f.typeCalls, "delete "+id)
	return f.typeErr
}

func (f *fake) RecordEmployee(_ context.Context, in service.EmployeeInput) (entitlement.Entitlement, error) {
	f.employee = in
	return entitlement.Entitlement{ID: "id-2", Subject: entitlement.PersonOf(in.Email)}, f.recordErr
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
	diplomas, _ := body["types"].([]any)
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
	code, body := call(setup(f), "POST", "/entitlements/type/laadpalen-management", `{"name":"Jerry","email":"j@example.com","validUntil":"31-12-2099"}`)

	if code != 200 || body["entitlementId"] != "id-1" || f.recordedType != "Diploma Laadpalen Management" || f.recorded["validUntil"] != "31-12-2099" {
		t.Fatalf("%d %v %+v", code, body, f.recorded)
	}
}

func TestSaysWhyARequestWasRefusedAndMailsNothing(t *testing.T) {
	f := &fake{recordErr: service.InvalidError{Message: "email is not an email address"}}
	code, body := call(setup(f), "POST", "/entitlements/type/laadpalen-management", `{"email":"x"}`)

	if code != 400 || body["error"] != "email is not an email address" || len(f.mailed) != 0 {
		t.Fatalf("%d %v mailed %v", code, body, f.mailed)
	}
}

func TestRefusesABodyThatIsNotJSON(t *testing.T) {
	f := &fake{}
	for _, path := range []string{"/entitlements/type/laadpalen-management", "/entitlements/employee"} {
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
	code, body := call(setup(f), "POST", "/entitlements/type/laadpalen-management", `{"name":"J","email":"j@example.com","validUntil":"31-12-2099"}`)

	if code != 502 || body["entitlementId"] != "id-1" || !strings.Contains(body["error"].(string), "vastgelegd") {
		t.Fatalf("%d %v", code, body)
	}
}

func TestAStorageErrorIsNotShownToThePages(t *testing.T) {
	code, body := call(setup(&fake{recordErr: errors.New("password authentication failed")}), "POST", "/entitlements/type/laadpalen-management", `{}`)

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
	for _, path := range []string{"/offers/diploma", "/offers/employee", "/entitlements/type/laadpalen-management/offer", "/login"} {
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

	for _, c := range []struct{ method, path string }{{"GET", "/info"}, {"POST", "/entitlements/employee"}, {"POST", "/entitlements/type/laadpalen-management"}} {
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
	if code, _ := call(setup(f), "POST", "/entitlements/type/koeien-melken", `{"name":"J","email":"j@example.com","validUntil":"31-12-2099"}`); code != 404 {
		t.Fatalf("an unknown diploma: %d", code)
	}
	if f.recordedType != "" {
		t.Fatal("an unknown diploma was recorded")
	}
}

func TestListsCreatesChangesAndDeletesCredentialTypes(t *testing.T) {
	f := &fake{}
	h := setup(f)
	code, body := call(h, "GET", "/credential-types", "")
	types, _ := body["types"].([]any)
	first, _ := types[0].(map[string]any)
	fields, _ := first["fields"].([]any)
	if code != 200 || len(types) != 1 || first["path"] != "laadpalen-management" || first["inUse"] != true || len(fields) != 1 {
		t.Fatalf("%d %v", code, body)
	}
	if code, body = call(h, "POST", "/credential-types", `{"title":"Badge","description":"d","fields":[{"key":"email","label":"E","kind":"email","required":true}]}`); code != 201 || body["id"] != "t2" {
		t.Fatalf("%d %v", code, body)
	}
	if code, body = call(h, "PUT", "/credential-types/t2", `{"title":"Badges","fields":[]}`); code != 200 || body["title"] != "Badges" {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ = call(h, "DELETE", "/credential-types/t2", ""); code != 204 {
		t.Fatalf("%d", code)
	}
	if got := strings.Join(f.typeCalls, "|"); got != "create Badge 1|update t2 Badges|delete t2" {
		t.Fatalf("calls %s", got)
	}
}

func TestAnswersWhyACredentialTypeCouldNotBeChanged(t *testing.T) {
	for err, want := range map[error]int{
		credtype.InvalidError{Message: "x"}: 400, credtype.ErrNotFound: 404, credtype.ErrExists: 409, credtype.ErrInUse: 409, errors.New("db down"): 500,
	} {
		code, body := call(setup(&fake{typeErr: err}), "DELETE", "/credential-types/t1", "")
		if code != want || strings.Contains(fmt.Sprint(body["error"]), "db down") {
			t.Errorf("%v: %d %v", err, code, body)
		}
	}
	if code, _ := call(setup(&fake{}), "POST", "/credential-types", "title=x"); code != 400 {
		t.Errorf("not JSON: %d", code)
	}
}

func TestSaysSoWhenTheRolesCannotBeRead(t *testing.T) {
	if code, _ := call(setup(&fake{rolesErr: errors.New("connection refused")}), "GET", "/info", ""); code != http.StatusInternalServerError {
		t.Fatalf("status %d", code)
	}
}

func TestListsWhatAPersonCanHaveRevoked(t *testing.T) {
	issued := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	f := &fake{active: []entitlement.Entitlement{
		{ID: "a", Type: "Employee", Status: entitlement.Issued, Claims: map[string]string{"name": "Jerry"}, IssuedAt: &issued},
		{ID: "b", Type: "Diploma Laadpalen Management", Status: entitlement.Claimed},
	}}
	code, body := call(setup(f), "GET", "/entitlements?email=jerry@example.com", "")

	list, _ := body["entitlements"].([]any)
	if code != 200 || len(list) != 2 {
		t.Fatalf("%d %v", code, body)
	}
	first, second := list[0].(map[string]any), list[1].(map[string]any)
	if first["id"] != "a" || first["status"] != "issued" || first["title"] != "Medewerker afdeling & rollen" || second["status"] != "waiting" {
		t.Fatalf("%v %v", first, second)
	}
	if code, _ := call(setup(f), "GET", "/entitlements?email=nope", ""); code != 400 {
		t.Fatalf("a bad email gave %d", code)
	}
}

func TestListsTheCredentialsOfEveryoneWithoutAnEmailAddress(t *testing.T) {
	f := &fake{active: []entitlement.Entitlement{{ID: "b", Type: "Diploma Laadpalen Management", Status: entitlement.Claimed}}}
	code, body := call(setup(f), "GET", "/entitlements", "")

	list, _ := body["entitlements"].([]any)
	if code != 200 || len(list) != 2 || list[0].(map[string]any)["id"] != "z" || list[1].(map[string]any)["status"] != "waiting" {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ := call(setup(f), "GET", "/entitlements?email=", ""); code != 400 {
		t.Fatalf("an empty email gave %d", code)
	}
}

func TestRevokesWithoutABody(t *testing.T) {
	f := &fake{}
	code, body := call(setup(f), "POST", "/entitlements/revoke/abc", "")

	if code != 200 || body["revoked"] != true || len(f.revoked) != 1 || f.revoked[0] != "abc" || f.replaced != "" || len(f.mailed) != 0 {
		t.Fatalf("%d %v %+v", code, body, f)
	}
}

func TestRevokesAndMailsTheCorrectedCredentialWhenGivenItsData(t *testing.T) {
	f := &fake{}
	code, body := call(setup(f), "POST", "/entitlements/revoke/abc", `{"name":"Jerry","email":"Jerry@Example.com","department":"secretariaat","organisation":"Utrecht","roles":["reader"]}`)

	if code != 200 || body["entitlementId"] != "id-3" || body["mailedTo"] != "jerry@example.com" || f.replaced != "abc" || len(f.revoked) != 0 || len(f.mailed) != 1 {
		t.Fatalf("%d %v %+v", code, body, f)
	}
	if f.replacement.Values["department"] != "secretariaat" || strings.Join(f.replacement.Roles, ",") != "reader" {
		t.Fatalf("replacement %+v", f.replacement)
	}
}

func TestSaysWhyARevocationWasRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		body string
		want int
	}{
		"unknown":  {service.NotFoundError{}, "", 404},
		"conflict": {service.AlreadyWaitingError{}, `{"email":"x@y.z"}`, 409},
		"invalid":  {service.InvalidError{Message: "x"}, `{"email":"x"}`, 400},
		"not json": {nil, "name=x", 400},
		"storage":  {errors.New("password authentication failed"), "", 500},
	} {
		f := &fake{revokeErr: tc.err}
		code, body := call(setup(f), "POST", "/entitlements/revoke/abc", tc.body)
		if code != tc.want || len(f.mailed) != 0 || strings.Contains(fmt.Sprint(body["error"]), "password") {
			t.Errorf("%s: %d %v", name, code, body)
		}
	}
}

func TestListsCreatesRenamesAndDeletesRoles(t *testing.T) {
	f := &fake{}
	h := setup(f)

	code, body := call(h, "GET", "/roles", "")
	roles, _ := body["roles"].([]any)
	first, _ := roles[0].(map[string]any)
	if code != 200 || len(roles) != 1 || first["name"] != "reader" || first["inUse"] != true {
		t.Fatalf("%d %v", code, body)
	}
	if code, body = call(h, "POST", "/roles", `{"name":"auditor"}`); code != 201 || body["id"] != "r2" {
		t.Fatalf("%d %v", code, body)
	}
	if code, body = call(h, "PUT", "/roles/r2", `{"name":"auditors"}`); code != 200 || body["name"] != "auditors" {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ = call(h, "DELETE", "/roles/r2", ""); code != 204 {
		t.Fatalf("%d", code)
	}
	if got := strings.Join(f.roleCalls, "|"); got != "create auditor|rename r2 auditors|delete r2" {
		t.Fatalf("calls %s", got)
	}
}

func TestAnswersWhyARoleCouldNotBeChanged(t *testing.T) {
	for err, want := range map[error]int{role.ErrInvalid: 400, role.ErrNotFound: 404, role.ErrExists: 409, role.ErrInUse: 409, errors.New("db down"): 500} {
		code, body := call(setup(&fake{roleErr: err}), "DELETE", "/roles/r1", "")
		if code != want {
			t.Errorf("%v: %d %v", err, code, body)
		}
	}
}

func TestServesTheWorkloadEndpointsOnlyWhenGivenThem(t *testing.T) {
	workloads := http.NewServeMux()
	workloads.HandleFunc("POST /workloads/offer", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("offered")) })
	workloads.HandleFunc("GET /workloads", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("listed")) })
	with := httpapi.Handler(httpapi.Config{Protocol: http.NewServeMux(), Workloads: workloads}, &fake{})
	for _, c := range []struct{ method, path string }{{"POST", "/workloads/offer"}, {"GET", "/workloads"}} {
		if code, _ := call(with, c.method, c.path, ""); code != http.StatusOK {
			t.Errorf("with workloads, %s %s: %d", c.method, c.path, code)
		}
	}
	without := httpapi.Handler(httpapi.Config{Protocol: http.NewServeMux()}, &fake{})
	if code, _ := call(without, "POST", "/workloads/offer", ""); code != http.StatusNotFound {
		t.Errorf("without workloads, POST /workloads/offer: %d, want 404", code)
	}
}
