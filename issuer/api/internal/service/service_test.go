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
	"github.com/makiuchi-d/gozxing"
	zxqrcode "github.com/makiuchi-d/gozxing/qrcode"

	"attesta/issuer/internal/credential"
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
	return fixture{svc: service.New(cfg, store, role.NewStore(pool), &offers{}, m), store: store, mailer: m, logged: logged}
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

	if want := "mailed a QR code for the Employee credential to " + e.Email; !strings.Contains(f.logged.String(), want) {
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
	if open, _ := f.store.FindOpenByEmail(context.Background(), e.Email); len(open) != 1 {
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

	if e.Type != "Employee" || e.Email != strings.ToLower(in.Email) || e.Status != entitlement.Open {
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
	a := service.DiplomaInput{Name: "Jim", Email: "jim-a@example.com", ValidUntil: "31-12-2099"}
	b := service.DiplomaInput{Name: "Jim", Email: "jim-b@example.com", ValidUntil: "2099-12-31"}
	ea, err := f.svc.RecordDiploma(context.Background(), credential.Diplomas[0], a)
	if err != nil {
		t.Fatal(err)
	}
	eb, err := f.svc.RecordDiploma(context.Background(), credential.Diplomas[0], b)
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
	diploma, err := f.svc.RecordDiploma(ctx, credential.Diplomas[0], service.DiplomaInput{Name: in.Name, Email: in.Email, ValidUntil: "31-12-2099"})
	if err != nil || diploma.ID == first.ID {
		t.Fatalf("another type was refused: %v", err)
	}
}

func TestRecordsADiploma(t *testing.T) {
	f := newFixture(t, nil)
	e, err := f.svc.RecordDiploma(context.Background(), credential.Diplomas[0], service.DiplomaInput{Name: "Jerry", Email: "J@Example.com", ValidUntil: "31-12-2099"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != "Diploma Laadpalen Management" || e.Email != "j@example.com" || e.ValidUntil == nil || e.Claims["validUntil"] != "2099-12-31" || e.Claims["course"] != "" || e.Claims["email"] != "j@example.com" || e.Claims["title"] != "" {
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
	if _, err := f.svc.RecordDiploma(context.Background(), credential.Diplomas[0], service.DiplomaInput{Name: "Jerry", Email: "j@example.com"}); err == nil {
		t.Error("a diploma without an end was recorded")
	}
	if _, err := f.svc.RecordDiploma(context.Background(), credential.Diplomas[0], service.DiplomaInput{Name: "J", Email: "x", ValidUntil: "31-12-2099"}); err == nil {
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
	old, err := f.store.Create(ctx, entitlement.New{Email: "ended@example.com", Type: "Employee", Claims: map[string]string{"name": "A"}, ValidUntil: &soon})
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
	laadpalen := credential.Diplomas[0]
	cows := credential.Diploma{Type: "DiplomaKoeienMelken", Path: "koeien-melken", Title: "Diploma Koeien Melken"}
	in := service.DiplomaInput{Name: "Jim", Email: "jim@example.com", ValidUntil: "31-12-2099"}
	first, err := f.svc.RecordDiploma(ctx, laadpalen, in)
	if err != nil {
		t.Fatal(err)
	}
	issue(t, f, first)

	other, err := f.svc.RecordDiploma(ctx, cows, in)
	if err != nil || other.Type != "DiplomaKoeienMelken" {
		t.Fatalf("a second diploma was refused or wrong: %v %+v", err, other)
	}
	var issued service.AlreadyIssuedError
	if _, err := f.svc.RecordDiploma(ctx, laadpalen, in); !errors.As(err, &issued) {
		t.Fatalf("the same diploma again: %v", err)
	}
	later := in
	later.ValidUntil = "31-12-2100"
	if _, err := f.svc.RecordDiploma(ctx, laadpalen, later); !errors.As(err, &issued) {
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
	base := service.DiplomaInput{Name: "J", Email: "j@example.com"}
	for name, day := range map[string]string{"none": "", "not a date": "soon", "past": "01-01-2020"} {
		in := base
		in.ValidUntil = day
		if _, err := f.svc.RecordDiploma(context.Background(), credential.Diplomas[0], in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
