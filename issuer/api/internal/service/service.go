// Package service is what the issuer does for the people who run it: record that a person is entitled to a credential, and mail them the QR code
// that gives it to them.
package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"maps"
	"regexp"
	"strings"
	"time"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/qr"
)

const qrSize = 480

type Store interface {
	Create(ctx context.Context, n entitlement.New) (entitlement.Entitlement, error)
	FindOpenByEmail(ctx context.Context, email string) ([]entitlement.Entitlement, error)
}

// Offerer makes the link a QR code holds, with a one-time code of its own.
type Offerer interface {
	Offer(ctx context.Context, e entitlement.Entitlement) (string, error)
}

type Config struct {
	// IssuerName is written in the mails, e.g. "Gemeente Utrecht".
	IssuerName string
	// QRLifetime is how long a QR code works, as the mail tells the person.
	QRLifetime time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
	// Log defaults to slog.Default().
	Log *slog.Logger
}

type Service struct {
	cfg    Config
	store  Store
	offers Offerer
	mailer mail.Mailer
}

func New(cfg Config, store Store, offers Offerer, mailer mail.Mailer) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Service{cfg: cfg, store: store, offers: offers, mailer: mailer}
}

// InvalidError is a request that cannot be recorded; its message says why.
type InvalidError struct{ Message string }

func (e InvalidError) Error() string { return e.Message }

type DiplomaInput struct {
	Name, Email, Degree, University string
	Description                     string
}

type EmployeeInput struct {
	Name, Email, Department, Organisation, Diploma string
	// DiplomaValidUntil is dd-mm-yyyy or YYYY-MM-DD; the credential ends at the end of that day.
	DiplomaValidUntil string
	Description       string
}

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+$`)

func blank(values ...string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return true
		}
	}
	return false
}

func (s *Service) description(given, credType string) string {
	if d := strings.TrimSpace(given); d != "" {
		return d
	}
	return credential.DefaultDescriptions[credType]
}

func (s *Service) RecordDiploma(ctx context.Context, in DiplomaInput) (entitlement.Entitlement, error) {
	if blank(in.Name, in.Email, in.Degree, in.University) {
		return entitlement.Entitlement{}, InvalidError{"name, email, degree and university are required"}
	}
	if !emailPattern.MatchString(in.Email) {
		return entitlement.Entitlement{}, InvalidError{"email is not an email address"}
	}
	claims := map[string]string{
		"name": in.Name, "email": in.Email, "degree": in.Degree, "university": in.University,
		"description": s.description(in.Description, credential.Diploma),
	}
	return s.record(ctx, entitlement.New{Email: in.Email, Type: credential.Diploma, Claims: claims})
}

func (s *Service) RecordEmployee(ctx context.Context, in EmployeeInput) (entitlement.Entitlement, error) {
	if blank(in.Name, in.Email, in.Department, in.Organisation, in.Diploma, in.DiplomaValidUntil) {
		return entitlement.Entitlement{}, InvalidError{"name, email, department, organisation, diploma and diplomaValidUntil are required"}
	}
	if !emailPattern.MatchString(in.Email) {
		return entitlement.Entitlement{}, InvalidError{"email is not an email address"}
	}
	if !contains(credential.Departments, in.Department) {
		return entitlement.Entitlement{}, InvalidError{"department must be one of " + strings.Join(credential.Departments, ", ")}
	}
	day, err := credential.ParseDate(in.DiplomaValidUntil)
	if err != nil {
		return entitlement.Entitlement{}, InvalidError{"diplomaValidUntil must be a date, dd-mm-yyyy or YYYY-MM-DD"}
	}
	end := credential.EndOfDay(day)
	if !end.After(s.cfg.Now()) {
		return entitlement.Entitlement{}, InvalidError{"diplomaValidUntil is in the past; a wallet refuses an expired credential"}
	}
	claims := map[string]string{
		"name": in.Name, "email": in.Email, "department": in.Department, "organisation": in.Organisation, "diploma": in.Diploma,
		"diplomaValidUntil": day.Format(time.DateOnly), "description": s.description(in.Description, credential.Employee),
	}
	return s.record(ctx, entitlement.New{Email: in.Email, Type: credential.Employee, Claims: claims, ValidUntil: &end})
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// record makes the entitlement; the same person with the same data again gets the entitlement they already have, while it is open.
func (s *Service) record(ctx context.Context, n entitlement.New) (entitlement.Entitlement, error) {
	open, err := s.store.FindOpenByEmail(ctx, n.Email)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	for _, e := range open {
		if e.Type == n.Type && maps.Equal(e.Claims, n.Claims) {
			return e, nil
		}
	}
	return s.store.Create(ctx, n)
}

// MailQRCode mails the person of the entitlement a QR code that gives them the credential.
func (s *Service) MailQRCode(ctx context.Context, e entitlement.Entitlement) error {
	link, err := s.offers.Offer(ctx, e)
	if err != nil {
		return err
	}
	png, err := qr.PNG(link, qrSize)
	if err != nil {
		return err
	}
	hours := max(1, int((s.cfg.QRLifetime+30*time.Minute)/time.Hour))
	where := ""
	if s.cfg.IssuerName != "" {
		where = " bij " + s.cfg.IssuerName
	}
	htmlWhere := html.EscapeString(where)
	err = s.mailer.Send(ctx, mail.Message{
		To:      e.Email,
		Subject: "Uw credential staat klaar" + where,
		Text: fmt.Sprintf("Er staat een credential voor u klaar%s.\n\nSla de bijlage (credential.png) op. Kies in uw wallet \"QR-code uploaden\" en kies dat bestand; de rest gaat vanzelf.\n\n"+
			"De QR-code is %d uur geldig en werkt één keer. Is hij verlopen? Dan stuurt de uitgever u automatisch een nieuwe.\nVerwachtte u dit niet? Dan kunt u dit bericht negeren.\n", where, hours),
		HTML: fmt.Sprintf(`<p>Er staat een credential voor u klaar%s.</p><p>Sla de afbeelding hieronder op. Kies in uw wallet <strong>QR-code uploaden</strong> en kies dat bestand; de rest gaat vanzelf.</p>`+
			`<p><img src="cid:credential-qr" alt="QR-code" width="240" height="240"></p><p>De QR-code is %d uur geldig en werkt één keer. Is hij verlopen? Dan stuurt de uitgever u automatisch een nieuwe.</p>`+
			`<p>Verwachtte u dit niet? Dan kunt u dit bericht negeren.</p>`, htmlWhere, hours),
		Image: &mail.Image{Filename: "credential.png", ContentID: "credential-qr", PNG: png},
	})
	if err != nil {
		return fmt.Errorf("mail the QR code: %w", err)
	}
	s.cfg.Log.Info(fmt.Sprintf("mailed a QR code for the %s credential to %s", e.Type, e.Email))
	return nil
}
