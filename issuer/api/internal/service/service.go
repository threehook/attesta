// Package service is what the issuer does for the people who run it: record that a person is entitled to a credential, and mail them the QR code
// that gives it to them.
package service

import (
	"context"
	"errors"
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
	"attesta/issuer/internal/role"
)

const qrSize = 480

type Store interface {
	Create(ctx context.Context, n entitlement.New) (entitlement.Entitlement, error)
	FindOpenByEmail(ctx context.Context, email string) ([]entitlement.Entitlement, error)
	FindIssuedByEmail(ctx context.Context, email string) ([]entitlement.Entitlement, error)
	FindActiveByEmail(ctx context.Context, email string) ([]entitlement.Entitlement, error)
	Find(ctx context.Context, id string) (entitlement.Entitlement, error)
	Revoke(ctx context.Context, id string) (bool, error)
	Replace(ctx context.Context, id string, n entitlement.New) (entitlement.Entitlement, error)
}

// RoleStore keeps the roles staff can give an employee.
type RoleStore interface {
	Names(ctx context.Context) ([]string, error)
	List(ctx context.Context) ([]role.Role, error)
	Create(ctx context.Context, name string) (role.Role, error)
	Rename(ctx context.Context, id, name string) (role.Role, error)
	Delete(ctx context.Context, id string) error
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
	n, err := s.diplomaOf(d, in)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	return s.record(ctx, n)
}

func (s *Service) RecordEmployee(ctx context.Context, in EmployeeInput) (entitlement.Entitlement, error) {
	n, err := s.employeeOf(ctx, in)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	return s.record(ctx, n)
}

func (s *Service) diplomaOf(d credential.Diploma, in DiplomaInput) (entitlement.New, error) {
	if blank(in.Name, in.Email, in.ValidUntil) {
		return entitlement.New{}, InvalidError{"name, email and validUntil are required"}
	}
	if !emailPattern.MatchString(in.Email) {
		return entitlement.New{}, InvalidError{"email is not an email address"}
	}
	end, day, err := s.endOf(in.ValidUntil, "validUntil")
	if err != nil {
		return entitlement.New{}, err
	}
	claims := map[string]string{
		"name": in.Name, "email": strings.ToLower(strings.TrimSpace(in.Email)), "validUntil": day,
	}
	return entitlement.New{Email: in.Email, Type: d.Type, Claims: claims, ValidUntil: &end}, nil
}

func (s *Service) employeeOf(ctx context.Context, in EmployeeInput) (entitlement.New, error) {
	if blank(in.Name, in.Email, in.Department, in.Organisation) {
		return entitlement.New{}, InvalidError{"name, email, department and organisation are required"}
	}
	if !emailPattern.MatchString(in.Email) {
		return entitlement.New{}, InvalidError{"email is not an email address"}
	}
	if !contains(credential.Departments, in.Department) {
		return entitlement.New{}, InvalidError{"department must be one of " + strings.Join(credential.Departments, ", ")}
	}
	known, err := s.roles.Names(ctx)
	if err != nil {
		return entitlement.New{}, err
	}
	roles := []string{}
	for _, r := range in.Roles {
		if !contains(known, r) {
			return entitlement.New{}, InvalidError{"roles must be among " + strings.Join(known, ", ")}
		}
		if !contains(roles, r) {
			roles = append(roles, r)
		}
	}
	claims := map[string]string{
		"name": in.Name, "email": strings.ToLower(strings.TrimSpace(in.Email)), "department": in.Department, "organisation": in.Organisation,
		"roles": strings.Join(roles, ", "),
	}
	return entitlement.New{Email: in.Email, Type: credential.Employee, Claims: claims}, nil
}

// NotFoundError is a credential that does not exist or is no longer there to revoke.
type NotFoundError struct{}

func (NotFoundError) Error() string { return "dit credential bestaat niet of is al ingetrokken" }

// Replacement is the corrected data of a credential: the fields of its form. Only the ones its type has are read.
type Replacement struct {
	Name, Email, Department, Organisation, ValidUntil string
	Roles                                             []string
}

// Active lists the credentials of the person that can be revoked: waiting to be fetched, or issued, and not ended.
func (s *Service) Active(ctx context.Context, email string) ([]entitlement.Entitlement, error) {
	if !emailPattern.MatchString(email) {
		return nil, InvalidError{"email is not an email address"}
	}
	return s.store.FindActiveByEmail(ctx, email)
}

// Revoke withdraws a credential: a wallet that holds it is refused from the next status list on, and a QR code for it stops working.
func (s *Service) Revoke(ctx context.Context, id string) error {
	if _, err := s.store.Find(ctx, id); errors.Is(err, entitlement.ErrNotFound) {
		return NotFoundError{}
	} else if err != nil {
		return err
	}
	ok, err := s.store.Revoke(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return NotFoundError{}
	}
	return nil
}

// Replace withdraws a credential and records a corrected one of the same type in the same step. The caller mails its QR code.
func (s *Service) Replace(ctx context.Context, id string, r Replacement) (entitlement.Entitlement, error) {
	old, err := s.store.Find(ctx, id)
	if errors.Is(err, entitlement.ErrNotFound) {
		return entitlement.Entitlement{}, NotFoundError{}
	}
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	var n entitlement.New
	if old.Type == credential.Employee {
		n, err = s.employeeOf(ctx, EmployeeInput{Name: r.Name, Email: r.Email, Department: r.Department, Organisation: r.Organisation, Roles: r.Roles})
	} else if d, ok := credential.DiplomaByType(old.Type); ok {
		n, err = s.diplomaOf(d, DiplomaInput{Name: r.Name, Email: r.Email, ValidUntil: r.ValidUntil})
	} else {
		err = fmt.Errorf("unknown credential type %q", old.Type)
	}
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	if err := s.stands(ctx, n, id); err != nil {
		return entitlement.Entitlement{}, err
	}
	e, err := s.store.Replace(ctx, id, n)
	if errors.Is(err, entitlement.ErrNotFound) {
		return entitlement.Entitlement{}, NotFoundError{}
	}
	return e, err
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

// ListRoles lists the roles with whether an employee holds them.
func (s *Service) ListRoles(ctx context.Context) ([]role.Role, error) { return s.roles.List(ctx) }

func (s *Service) CreateRole(ctx context.Context, name string) (role.Role, error) {
	return s.roles.Create(ctx, name)
}

// DeleteRole refuses a role an employee holds (role.ErrInUse).
func (s *Service) RenameRole(ctx context.Context, id, name string) (role.Role, error) {
	return s.roles.Rename(ctx, id, name)
}

func (s *Service) DeleteRole(ctx context.Context, id string) error { return s.roles.Delete(ctx, id) }

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
	if err := s.stands(ctx, n, ""); err != nil {
		return entitlement.Entitlement{}, err
	}
	return s.store.Create(ctx, n)
}

// stands reports the credential of the person's that stands in the way of n: one of the same type that is issued, or waiting with other data.
// The one with the id except does not count, and neither does one waiting with the same data, which Create gives back.
func (s *Service) stands(ctx context.Context, n entitlement.New, except string) error {
	open, err := s.store.FindOpenByEmail(ctx, n.Email)
	if err != nil {
		return err
	}
	for _, e := range open {
		if e.Type != n.Type || e.ID == except {
			continue
		}
		if maps.Equal(e.Claims, n.Claims) {
			return nil
		}
		return AlreadyWaitingError{}
	}
	issued, err := s.store.FindIssuedByEmail(ctx, n.Email)
	if err != nil {
		return err
	}
	for _, e := range issued {
		if e.Type == n.Type && e.ID != except {
			return AlreadyIssuedError{IssuedAt: *e.IssuedAt}
		}
	}
	return nil
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
