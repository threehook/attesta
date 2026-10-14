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
	FindIssuedByEmail(ctx context.Context, email string) ([]entitlement.Entitlement, error)
}

// RoleStore lists the roles staff can give an employee.
type RoleStore interface {
	Names(ctx context.Context) ([]string, error)
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
	roles  RoleStore
	offers Offerer
	mailer mail.Mailer
}

func New(cfg Config, store Store, roles RoleStore, offers Offerer, mailer mail.Mailer) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Service{cfg: cfg, store: store, roles: roles, offers: offers, mailer: mailer}
}

// InvalidError is a request that cannot be recorded; its message says why.
type InvalidError struct{ Message string }

func (e InvalidError) Error() string { return e.Message }

// AlreadyIssuedError is a request for a credential of a type the person already has and that has not ended: it must be revoked first.
type AlreadyIssuedError struct{ IssuedAt time.Time }

func (e AlreadyIssuedError) Error() string {
	return "deze persoon heeft al een geldig credential van dit type (uitgegeven op " + e.IssuedAt.Format("02-01-2006") + "); dat moet eerst worden ingetrokken"
}

// AlreadyWaitingError is the same for a credential that is recorded but not yet fetched, with other data than the request.
type AlreadyWaitingError struct{}

func (AlreadyWaitingError) Error() string {
	return "voor deze persoon staat al een credential van dit type klaar met andere gegevens; dat moet eerst worden ingetrokken"
}

type DiplomaInput struct {
	Name, Email string
	// ValidUntil is dd-mm-yyyy or YYYY-MM-DD; the credential ends at the end of that day.
	ValidUntil string
}

type EmployeeInput struct {
	Name, Email, Department, Organisation string
	// Roles are the employee's roles, each one of credential.Roles; they may be none.
	Roles []string
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

func (s *Service) RecordDiploma(ctx context.Context, d credential.Diploma, in DiplomaInput) (entitlement.Entitlement, error) {
	if blank(in.Name, in.Email, in.ValidUntil) {
		return entitlement.Entitlement{}, InvalidError{"name, email and validUntil are required"}
	}
	if !emailPattern.MatchString(in.Email) {
		return entitlement.Entitlement{}, InvalidError{"email is not an email address"}
	}
	end, day, err := s.endOf(in.ValidUntil, "validUntil")
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	claims := map[string]string{
		"name": in.Name, "email": strings.ToLower(strings.TrimSpace(in.Email)), "validUntil": day,
	}
	return s.record(ctx, entitlement.New{Email: in.Email, Type: d.Type, Claims: claims, ValidUntil: &end})
}

func (s *Service) RecordEmployee(ctx context.Context, in EmployeeInput) (entitlement.Entitlement, error) {
	if blank(in.Name, in.Email, in.Department, in.Organisation) {
		return entitlement.Entitlement{}, InvalidError{"name, email, department and organisation are required"}
	}
	if !emailPattern.MatchString(in.Email) {
		return entitlement.Entitlement{}, InvalidError{"email is not an email address"}
	}
	if !contains(credential.Departments, in.Department) {
		return entitlement.Entitlement{}, InvalidError{"department must be one of " + strings.Join(credential.Departments, ", ")}
	}
	known, err := s.roles.Names(ctx)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	roles := []string{}
	for _, r := range in.Roles {
		if !contains(known, r) {
			return entitlement.Entitlement{}, InvalidError{"roles must be among " + strings.Join(known, ", ")}
		}
		if !contains(roles, r) {
			roles = append(roles, r)
		}
	}
	claims := map[string]string{
		"name": in.Name, "email": strings.ToLower(strings.TrimSpace(in.Email)), "department": in.Department, "organisation": in.Organisation,
		"roles": strings.Join(roles, ", "),
	}
	n := entitlement.New{Email: in.Email, Type: credential.Employee, Claims: claims}
	return s.record(ctx, n)
}

// endOf reads the day a credential ends, which must be in the future: a wallet refuses an expired credential.
func (s *Service) endOf(value, field string) (end time.Time, day string, err error) {
	d, err := credential.ParseDate(value)
	if err != nil {
		return time.Time{}, "", InvalidError{field + " must be a date, dd-mm-yyyy or YYYY-MM-DD"}
	}
	end = credential.EndOfDay(d)
	if !end.After(s.cfg.Now()) {
		return time.Time{}, "", InvalidError{field + " is in the past; a wallet refuses an expired credential"}
	}
	return end, d.Format(time.DateOnly), nil
}

// Roles lists the roles staff can give an employee.
func (s *Service) Roles(ctx context.Context) ([]string, error) { return s.roles.Names(ctx) }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// record makes the entitlement. A person has one of each credential at a time (one of each credential type): the same data again gives the entitlement that is waiting (and its QR code is
// mailed again), anything else is refused while one is waiting or issued and not ended.
func (s *Service) record(ctx context.Context, n entitlement.New) (entitlement.Entitlement, error) {
	open, err := s.store.FindOpenByEmail(ctx, n.Email)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	for _, e := range open {
		if e.Type != n.Type {
			continue
		}
		if maps.Equal(e.Claims, n.Claims) {
			return e, nil
		}
		return entitlement.Entitlement{}, AlreadyWaitingError{}
	}
	issued, err := s.store.FindIssuedByEmail(ctx, n.Email)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	for _, e := range issued {
		if e.Type == n.Type {
			return entitlement.Entitlement{}, AlreadyIssuedError{IssuedAt: *e.IssuedAt}
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
