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

	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
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
	return fixture{svc: service.New(cfg, store, &offers{}, m), store: store, mailer: m, logged: logged}
}

func employee() service.EmployeeInput {
	return service.EmployeeInput{
		Name: "Jerry Smith", Email: uuid.NewString() + "@example.com", Department: "burgerzaken", Organisation: "Gemeente Utrecht",
		Diploma: "Laadpalen", DiplomaValidUntil: "31-12-2099",
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
	if e.Claims["diplomaValidUntil"] != "2099-12-31" || e.Claims["department"] != "burgerzaken" || e.Claims["description"] == "" {
		t.Fatalf("claims %v", e.Claims)
	}
	if want := time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC); e.ValidUntil == nil || !e.ValidUntil.Equal(want) {
		t.Fatalf("valid until %v", e.ValidUntil)
	}
}

func TestAcceptsBothDateFormatsAndKeepsAGivenDescription(t *testing.T) {
	f := newFixture(t, nil)
	a, b := employee(), employee()
	b.DiplomaValidUntil, b.Description = "2099-12-31", "  Mijn tekst "
	ea, err := f.svc.RecordEmployee(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	eb, err := f.svc.RecordEmployee(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if ea.Claims["diplomaValidUntil"] != eb.Claims["diplomaValidUntil"] || eb.Claims["description"] != "Mijn tekst" {
		t.Fatalf("%v %v", ea.Claims, eb.Claims)
	}
}

func TestMakesOneEntitlementForTheSamePersonAndCredentialAndAnotherForADifferentOne(t *testing.T) {
	f := newFixture(t, nil)
	in := employee()
	first, _ := f.svc.RecordEmployee(context.Background(), in)
	again, _ := f.svc.RecordEmployee(context.Background(), in)
	other := in
	other.Department = "secretariaat"
	different, _ := f.svc.RecordEmployee(context.Background(), other)

	if first.ID != again.ID {
		t.Fatal("the same request made two entitlements")
	}
	if first.ID == different.ID {
		t.Fatal("a different request got the same entitlement")
	}
}

func TestRecordsADiploma(t *testing.T) {
	f := newFixture(t, nil)
	e, err := f.svc.RecordDiploma(context.Background(), service.DiplomaInput{Name: "Jerry", Email: "J@Example.com", Degree: "Wiskunde", University: "UU"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != "Diploma" || e.Email != "j@example.com" || e.ValidUntil != nil || e.Claims["degree"] != "Wiskunde" {
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
		"not a date":         with(func(in *service.EmployeeInput) { in.DiplomaValidUntil = "morgen" }),
		"a date in the past": with(func(in *service.EmployeeInput) { in.DiplomaValidUntil = "01-01-2020" }),
	}
	for name, in := range cases {
		_, err := f.svc.RecordEmployee(context.Background(), in)
		var invalid service.InvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err %v", name, err)
		}
	}
	if _, err := f.svc.RecordDiploma(context.Background(), service.DiplomaInput{Name: "Jerry", Email: "j@example.com"}); err == nil {
		t.Error("a diploma without degree and university was recorded")
	}
	if _, err := f.svc.RecordDiploma(context.Background(), service.DiplomaInput{Name: "J", Email: "x", Degree: "d", University: "u"}); err == nil {
		t.Error("a diploma with a bad email was recorded")
	}
}
