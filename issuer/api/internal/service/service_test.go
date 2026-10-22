package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/makiuchi-d/gozxing"
	zxqrcode "github.com/makiuchi-d/gozxing/qrcode"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/credtype"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
	"attesta/issuer/internal/role"
	"attesta/issuer/internal/service"
)

var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

type mailer struct {
	sent []mail.Message
	err  error
}

func (m *mailer) Send(_ context.Context, msg mail.Message) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

// offers makes a link of its own for every QR code, as the issuer does with a new one-time code.
type offers struct{ n int }

func (o *offers) Offer(_ context.Context, e entitlement.Entitlement) (string, error) {
	o.n++
	return fmt.Sprintf("openid-credential-offer://?credential_offer_uri=https%%3A%%2F%%2Fissuer.example%%2Foid4vci%%2Foffers%%2F%s-%d", e.ID, o.n), nil
}

type fixture struct {
	svc    *service.Service
	store  *entitlement.Store
	mailer *mailer
	logged *strings.Builder
	pool   *pgxpool.Pool
}

func newFixture(t *testing.T, edit func(*service.Config)) fixture {
	t.Helper()
	pool, err := postgres.Open(context.Background(), postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// The roles the tests give; the table itself starts with laadpalen-aanvrager only.
	if _, err := pool.Exec(context.Background(), "INSERT INTO roles (name) VALUES ('reader') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	store := entitlement.NewStore(pool, 0)
	m := &mailer{}
	logged := &strings.Builder{}
	cfg := service.Config{
		IssuerName: "Gemeente Utrecht", QRLifetime: 24 * time.Hour, Now: func() time.Time { return now },
		Log: slog.New(slog.NewTextHandler(logged, nil)),
	}
	if edit != nil {
		edit(&cfg)
	}
	return fixture{svc: service.New(cfg, store, role.NewStore(pool), credtype.NewStore(pool), &offers{}, m), store: store, mailer: m, logged: logged, pool: pool}
}

var laadpalen = credential.Type{
	Type: "Diploma Laadpalen Management", Path: "laadpalen-management", Title: "Diploma Laadpalen Management",
	Fields: []credential.Field{
		{Key: "name", Label: "Naam", Kind: "text", Required: true},
		{Key: "email", Label: "E-mailadres", Kind: "email", Required: true},
		{Key: "validUntil", Label: "Geldig tot", Kind: "date", Required: true},
	},
}

func diplomaAnswers(name, email, validUntil string) map[string]string {
	return map[string]string{"name": name, "email": email, "validUntil": validUntil}
}

func employee() service.EmployeeInput {
	return service.EmployeeInput{
		Name: "Jerry Smith", Email: uuid.NewString() + "@example.com", Department: "burgerzaken", Organisation: "Gemeente Utrecht",
		Roles: []string{"laadpalen-aanvrager"},
	}
}

func TestMailsTheQRCodeOnceAsAnImageInTheMail(t *testing.T) {
	f := newFixture(t, nil)
	in := employee()
	e, err := f.svc.RecordEmployee(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.MailQRCode(context.Background(), e); err != nil {
		t.Fatal(err)
	}

	if len(f.mailer.sent) != 1 {
		t.Fatalf("%d mails", len(f.mailer.sent))
	}
	m := f.mailer.sent[0]
	if m.To != strings.ToLower(in.Email) || m.Subject != "Uw credential staat klaar bij Gemeente Utrecht" {
		t.Fatalf("mail %+v", m)
	}
	if m.Image == nil || m.Image.Filename != "credential.png" || !strings.Contains(m.HTML, "cid:"+m.Image.ContentID) {
		t.Fatalf("image %+v", m.Image)
	}
	for _, text := range []string{m.Text, m.HTML} {
		if !strings.Contains(text, "24 uur") || !strings.Contains(text, "QR-code uploaden") {
			t.Errorf("the mail does not say how long the code works or what to do:\n%s", text)
		}
	}
}

func TestHoldsTheLinkInTheCodeAndNothingSecretInTheText(t *testing.T) {
	f := newFixture(t, nil)
	e, _ := f.svc.RecordEmployee(context.Background(), employee())
	if err := f.svc.MailQRCode(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	m := f.mailer.sent[0]

	img, _, err := image.Decode(bytes.NewReader(m.Image.PNG))
	if err != nil {
		t.Fatal(err)
	}
	bitmap, _ := gozxing.NewBinaryBitmapFromImage(img)
	decoded, err := zxqrcode.NewQRCodeReader().Decode(bitmap, nil)
	if err != nil {
		t.Fatal(err)
	}
	link := decoded.GetText()
	if !strings.HasPrefix(link, "openid-credential-offer://?credential_offer_uri=") {
		t.Fatalf("the code holds %q", link)
	}
	for _, text := range []string{m.Text, m.HTML} {
		if strings.Contains(text, "openid-credential-offer") || strings.Contains(text, "offers") {
			t.Errorf("the link is in the text of the mail:\n%s", text)
		}
	}
}

func TestEveryMailingHasACodeOfItsOwn(t *testing.T) {
	f := newFixture(t, nil)
	e, _ := f.svc.RecordEmployee(context.Background(), employee())
	f.svc.MailQRCode(context.Background(), e)
	f.svc.MailQRCode(context.Background(), e)

	if len(f.mailer.sent) != 2 || bytes.Equal(f.mailer.sent[0].Image.PNG, f.mailer.sent[1].Image.PNG) {
		t.Fatal("the second mail has the same code as the first")
	}
}

func TestLogsThatItMailedAQRCodeAndToWhom(t *testing.T) {
	f := newFixture(t, nil)
	e, _ := f.svc.RecordEmployee(context.Background(), employee())
	f.svc.MailQRCode(context.Background(), e)

	if want := "mailed a QR code for the Employee credential to " + e.Subject.Email; !strings.Contains(f.logged.String(), want) {
		t.Fatalf("log %q lacks %q", f.logged.String(), want)
	}
}

func TestKeepsTheEntitlementWhenTheMailCannotBeSent(t *testing.T) {
	f := newFixture(t, nil)
	e, _ := f.svc.RecordEmployee(context.Background(), employee())
	f.mailer.err = errors.New("connection refused")

	if err := f.svc.MailQRCode(context.Background(), e); err == nil {
		t.Fatal("no error")
	}
	if open, _ := f.store.FindOpenFor(context.Background(), e.Subject); len(open) != 1 {
		t.Fatalf("%d open entitlements", len(open))
	}
}

func TestTellsAShortLifetimeAsAtLeastAnHour(t *testing.T) {
	f := newFixture(t, func(c *service.Config) { c.QRLifetime = 5 * time.Minute })
	e, _ := f.svc.RecordEmployee(context.Background(), employee())
	f.svc.MailQRCode(context.Background(), e)

	if !strings.Contains(f.mailer.sent[0].Text, "1 uur geldig") {
		t.Fatalf("text:\n%s", f.mailer.sent[0].Text)
	}
}

func TestRecordsAnEmployeeWithTheClaimsOnFile(t *testing.T) {
	f := newFixture(t, nil)
	in := employee()
	e, err := f.svc.RecordEmployee(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	if e.Type != "Employee" || e.Subject.Email != strings.ToLower(in.Email) || e.Status != entitlement.Open {
		t.Fatalf("entitlement %+v", e)
	}
	if e.Claims["validUntil"] != "" || e.Claims["roles"] != "laadpalen-aanvrager" || e.Claims["diploma"] != "" || e.Claims["department"] != "burgerzaken" || e.Claims["title"] != "" || e.Claims["description"] != "" {
		t.Fatalf("claims %v", e.Claims)
	}
	if e.ValidUntil != nil {
		t.Fatalf("an Employee must not end, valid until %v", e.ValidUntil)
	}
}

func TestAcceptsBothDateFormatsForADiploma(t *testing.T) {
	f := newFixture(t, nil)
	a := diplomaAnswers("Jim", "jim-a@example.com", "31-12-2099")
	b := diplomaAnswers("Jim", "jim-b@example.com", "2099-12-31")
	ea, err := f.svc.RecordType(context.Background(), laadpalen, a)
	if err != nil {
		t.Fatal(err)
	}
	eb, err := f.svc.RecordType(context.Background(), laadpalen, b)
	if err != nil {
		t.Fatal(err)
	}
	if ea.Claims["validUntil"] != "2099-12-31" || eb.Claims["validUntil"] != "2099-12-31" {
		t.Fatalf("%v %v", ea.Claims, eb.Claims)
	}
}

func TestMakesOneEntitlementForTheSamePersonAndCredentialAndRefusesOtherDataWhileItWaits(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	in := employee()
	first, _ := f.svc.RecordEmployee(ctx, in)
	again, _ := f.svc.RecordEmployee(ctx, in)
	other := in
	other.Department = "secretariaat"
	_, err := f.svc.RecordEmployee(ctx, other)

	if first.ID != again.ID {
		t.Fatal("the same request made two entitlements")
	}
	if !errors.As(err, &service.AlreadyWaitingError{}) {
		t.Fatalf("error %v", err)
	}
	diploma, err := f.svc.RecordType(ctx, laadpalen, diplomaAnswers(in.Name, in.Email, "31-12-2099"))
	if err != nil || diploma.ID == first.ID {
		t.Fatalf("another type was refused: %v", err)
	}
}

func TestRecordsADiploma(t *testing.T) {
	f := newFixture(t, nil)
	e, err := f.svc.RecordType(context.Background(), laadpalen, diplomaAnswers("Jerry", "J@Example.com", "31-12-2099"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != "Diploma Laadpalen Management" || e.Subject.Email != "j@example.com" || e.ValidUntil == nil || e.Claims["validUntil"] != "2099-12-31" || e.Claims["course"] != "" || e.Claims["email"] != "j@example.com" || e.Claims["title"] != "" {
		t.Fatalf("entitlement %+v", e)
	}
}

func TestRefusesWhatCannotBeRecorded(t *testing.T) {
	f := newFixture(t, nil)
	with := func(edit func(*service.EmployeeInput)) service.EmployeeInput {
		in := employee()
		edit(&in)
		return in
	}
	cases := map[string]service.EmployeeInput{
		"missing field":      with(func(in *service.EmployeeInput) { in.Organisation = "  " }),
		"not an email":       with(func(in *service.EmployeeInput) { in.Email = "jerry" }),
		"another department": with(func(in *service.EmployeeInput) { in.Department = "sloop" }),
	}
	for name, in := range cases {
		_, err := f.svc.RecordEmployee(context.Background(), in)
		var invalid service.InvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err %v", name, err)
		}
	}
	if _, err := f.svc.RecordType(context.Background(), laadpalen, diplomaAnswers("Jerry", "j@example.com", "")); err == nil {
		t.Error("a diploma without an end was recorded")
	}
	if _, err := f.svc.RecordType(context.Background(), laadpalen, diplomaAnswers("J", "x", "31-12-2099")); err == nil {
		t.Error("a diploma with a bad email was recorded")
	}
}

func issue(t *testing.T, f fixture, e entitlement.Entitlement) {
	t.Helper()
	if ok, err := f.store.Claim(context.Background(), e.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := f.store.MarkIssued(context.Background(), e.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestRefusesACredentialOfATypeThePersonStillHasWhateverTheData(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	in := employee()
	first, _ := f.svc.RecordEmployee(ctx, in)
	issue(t, f, first)

	for name, edit := range map[string]func(*service.EmployeeInput){
		"same data":        func(*service.EmployeeInput) {},
		"other department": func(i *service.EmployeeInput) { i.Department = "secretariaat" },
		"other roles":      func(i *service.EmployeeInput) { i.Roles = []string{"reader"} },
	} {
		other := in
		edit(&other)
		_, err := f.svc.RecordEmployee(ctx, other)
		var issued service.AlreadyIssuedError
		if !errors.As(err, &issued) || issued.IssuedAt.IsZero() {
			t.Errorf("%s: error %v", name, err)
		}
	}
}

func TestAllowsACredentialOnceTheOldOneHasEnded(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	soon := time.Now().Add(500 * time.Millisecond)
	old, err := f.store.Create(ctx, entitlement.New{Subject: entitlement.PersonOf("ended@example.com"), Type: "Employee", Claims: map[string]string{"name": "A"}, ValidUntil: &soon})
	if err != nil {
		t.Fatal(err)
	}
	issue(t, f, old)
	time.Sleep(600 * time.Millisecond)

	in := employee()
	in.Email = "ended@example.com"
	if _, err := f.svc.RecordEmployee(ctx, in); err != nil {
		t.Fatalf("refused although the old credential ended: %v", err)
	}
}

func TestAPersonCanHoldSeveralDiplomasButOnlyOneOfEach(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	cows := credential.Type{Type: "DiplomaKoeienMelken", Path: "koeien-melken", Title: "Diploma Koeien Melken", Fields: laadpalen.Fields}
	in := diplomaAnswers("Jim", "jim@example.com", "31-12-2099")
	first, err := f.svc.RecordType(ctx, laadpalen, in)
	if err != nil {
		t.Fatal(err)
	}
	issue(t, f, first)

	other, err := f.svc.RecordType(ctx, cows, in)
	if err != nil || other.Type != "DiplomaKoeienMelken" {
		t.Fatalf("a second diploma was refused or wrong: %v %+v", err, other)
	}
	var issued service.AlreadyIssuedError
	if _, err := f.svc.RecordType(ctx, laadpalen, in); !errors.As(err, &issued) {
		t.Fatalf("the same diploma again: %v", err)
	}
	later := diplomaAnswers("Jim", "jim@example.com", "31-12-2100")
	if _, err := f.svc.RecordType(ctx, laadpalen, later); !errors.As(err, &issued) {
		t.Fatalf("the same diploma with another end: %v", err)
	}
}

func TestRecordsTheRolesOfAnEmployeeFromTheOnesThatExistWithoutDuplicates(t *testing.T) {
	f := newFixture(t, nil)
	in := employee()
	in.Roles = []string{"laadpalen-aanvrager", "reader", "reader"}
	e, err := f.svc.RecordEmployee(context.Background(), in)
	if err != nil || e.Claims["roles"] != "laadpalen-aanvrager, reader" {
		t.Fatalf("%v %v", e.Claims, err)
	}

	for _, roles := range [][]string{{"baas"}, {"reader", "everything"}, {""}} {
		bad := employee()
		bad.Roles = roles
		var invalid service.InvalidError
		if _, err := f.svc.RecordEmployee(context.Background(), bad); !errors.As(err, &invalid) {
			t.Errorf("roles %v: %v", roles, err)
		}
	}
	none := employee()
	none.Roles = nil
	if e, err := f.svc.RecordEmployee(context.Background(), none); err != nil || e.Claims["roles"] != "" {
		t.Fatalf("no roles: %v %v", e.Claims, err)
	}
}

func TestRefusesADiplomaWithoutAFutureEndDate(t *testing.T) {
	f := newFixture(t, nil)
	for name, day := range map[string]string{"none": "", "not a date": "soon", "past": "01-01-2020"} {
		in := diplomaAnswers("J", "j@example.com", day)
		if _, err := f.svc.RecordType(context.Background(), laadpalen, in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRevokesACredentialSoThePersonCanGetANewOne(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	in := employee()
	first, _ := f.svc.RecordEmployee(ctx, in)
	issue(t, f, first)

	if err := f.svc.Revoke(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Revoke(ctx, first.ID); !errors.As(err, &service.NotFoundError{}) {
		t.Fatalf("revoking twice: %v", err)
	}
	if err := f.svc.Revoke(ctx, uuid.NewString()); !errors.As(err, &service.NotFoundError{}) {
		t.Fatalf("unknown id: %v", err)
	}
	if active, err := f.svc.Active(ctx, in.Email); err != nil || len(active) != 0 {
		t.Fatal(active, err)
	}
	if _, err := f.svc.RecordEmployee(ctx, in); err != nil {
		t.Fatalf("a new credential after revoking: %v", err)
	}
}

func TestReplacesACredentialWithACorrectedOneInOneStep(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	in := employee()
	old, _ := f.svc.RecordEmployee(ctx, in)
	issue(t, f, old)

	corrected, err := f.svc.Replace(ctx, old.ID, service.Replacement{
		Values: map[string]string{"name": in.Name, "email": in.Email, "department": "secretariaat", "organisation": in.Organisation}, Roles: in.Roles,
	})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.ID == old.ID || corrected.Claims["department"] != "secretariaat" || corrected.Type != credential.Employee {
		t.Fatalf("corrected %+v", corrected)
	}
	active, _ := f.svc.Active(ctx, in.Email)
	if len(active) != 1 || active[0].ID != corrected.ID {
		t.Fatalf("active %+v", active)
	}
	if gone, _ := f.store.Find(ctx, old.ID); gone.Status != entitlement.Revoked {
		t.Fatalf("old is %s", gone.Status)
	}
}

func TestReplacesADiplomaWithItsOwnFields(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	email := uuid.NewString() + "@example.com"
	old, err := f.svc.RecordType(ctx, laadpalen, diplomaAnswers("Jerry", email, "31-12-2099"))
	if err != nil {
		t.Fatal(err)
	}
	corrected, err := f.svc.Replace(ctx, old.ID, service.Replacement{Values: diplomaAnswers("Jerry Smith", email, "31-12-2098")})
	if err != nil || corrected.Type != laadpalen.Type || corrected.Claims["name"] != "Jerry Smith" {
		t.Fatal(corrected, err)
	}
}

func TestReplaceChangesNothingWhenTheCorrectionIsRefused(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	in := employee()
	old, _ := f.svc.RecordEmployee(ctx, in)
	issue(t, f, old)

	_, err := f.svc.Replace(ctx, old.ID, service.Replacement{Values: map[string]string{"name": in.Name, "email": "nope", "department": "burgerzaken", "organisation": in.Organisation}})
	var invalid service.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("err %v", err)
	}
	if kept, _ := f.store.Find(ctx, old.ID); kept.Status != entitlement.Issued {
		t.Fatalf("the credential is %s", kept.Status)
	}
}

func TestReplaceRefusesAnotherEmailAddressAndKeepsTheCredential(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	mine := employee()
	old, _ := f.svc.RecordEmployee(ctx, mine)
	issue(t, f, old)

	var invalid service.InvalidError
	_, err := f.svc.Replace(ctx, old.ID, service.Replacement{Values: map[string]string{"name": mine.Name, "email": employee().Email, "department": "burgerzaken", "organisation": mine.Organisation}})
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "e-mailadres") {
		t.Fatalf("err %v", err)
	}
	if kept, _ := f.store.Find(ctx, old.ID); kept.Status != entitlement.Issued {
		t.Fatalf("the credential is %s", kept.Status)
	}
	same, err := f.svc.Replace(ctx, old.ID, service.Replacement{Values: map[string]string{"name": mine.Name, "email": strings.ToUpper(mine.Email), "department": "secretariaat", "organisation": mine.Organisation}})
	if err != nil || same.Claims["department"] != "secretariaat" {
		t.Fatalf("the same address in other case: %+v %v", same, err)
	}
}

var badge = credential.Type{
	Type: "Badge", Path: "badge", Title: "Badge",
	Fields: []credential.Field{
		{Key: "email", Label: "E-mailadres", Kind: "email", Required: true},
		{Key: "colour", Label: "Kleur", Kind: "choice", Required: true, Options: []string{"red", "blue"}},
		{Key: "floor", Label: "Verdieping", Kind: "text"},
		{Key: "since", Label: "Sinds", Kind: "date"},
		{Key: "price", Label: "Prijs", Kind: "number", Decimals: 2, Min: "0", Max: "100"},
	},
}

func TestRecordsAnyTypeFromItsFieldsAndLeavesOutWhatIsNotAField(t *testing.T) {
	f := newFixture(t, nil)
	e, err := f.svc.RecordType(context.Background(), badge, map[string]string{
		"email": " Pat@Example.com ", "colour": "red", "since": "01-02-2020", "floor": " ", "extra": "x", "price": "12,5",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"email": "pat@example.com", "colour": "red", "since": "2020-02-01"}
	if e.Type != "Badge" || e.Subject.Email != "pat@example.com" || e.ValidUntil != nil || len(e.Claims) != 4 || e.Claims["price"] != "12.50" || e.Claims["email"] != want["email"] || e.Claims["colour"] != "red" || e.Claims["since"] != want["since"] {
		t.Fatalf("%+v", e)
	}
}

func TestRefusesAnswersThatDoNotFitTheFields(t *testing.T) {
	f := newFixture(t, nil)
	good := map[string]string{"email": "pat@example.com", "colour": "red"}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for key, val := range good {
			m[key] = val
		}
		m[k] = v
		return m
	}
	cases := map[string]map[string]string{
		"email missing":  with("email", ""),
		"not an email":   with("email", "pat"),
		"colour missing": with("colour", ""),
		"unknown colour": with("colour", "green"),
		"not a date":     with("since", "soon"),
		"text too long":  with("floor", strings.Repeat("x", credential.MaxValueSize+1)),
	}
	for name, values := range cases {
		var invalid service.InvalidError
		if _, err := f.svc.RecordType(context.Background(), badge, values); !errors.As(err, &invalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCorrectsACredentialOfATypeStaffDefined(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	title := "Pas " + uuid.NewString()
	def, err := credtype.NewStore(f.pool).Create(ctx, credtype.Input{Title: title, Fields: badge.Fields})
	if err != nil {
		t.Fatal(err)
	}
	old, err := f.svc.RecordType(ctx, def.Type, map[string]string{"email": "pat@example.com", "colour": "red"})
	if err != nil {
		t.Fatal(err)
	}

	corrected, err := f.svc.Replace(ctx, old.ID, service.Replacement{Values: map[string]string{"email": "pat@example.com", "colour": "blue"}})
	if err != nil || corrected.Type != title || corrected.Claims["colour"] != "blue" {
		t.Fatalf("%+v %v", corrected, err)
	}
}
