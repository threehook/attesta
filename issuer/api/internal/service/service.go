// Package service is what the issuer does for the people who run it: record that a person is entitled to a credential, and mail them the QR code
// that gives it to them.
package service

import (
	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/credtype"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/qr"
	"attesta/issuer/internal/role"
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
)

const qrSize = 480

type Store interface {
	Create(ctx context.Context, n entitlement.New) (entitlement.Entitlement, error)
	FindOpenFor(ctx context.Context, subject entitlement.Subject) ([]entitlement.Entitlement, error)
	FindIssuedFor(ctx context.Context, subject entitlement.Subject) ([]entitlement.Entitlement, error)
	FindActiveFor(ctx context.Context, subject entitlement.Subject) ([]entitlement.Entitlement, error)
	FindActive(ctx context.Context) ([]entitlement.Entitlement, error)
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

// TypeStore keeps the credential types staff define besides Employee.
type TypeStore interface {
	List(ctx context.Context) ([]credtype.Type, error)
	Find(ctx context.Context, typeName string) (credential.Type, error)
	FindByPath(ctx context.Context, path string) (credtype.Type, error)
	Create(ctx context.Context, in credtype.Input) (credtype.Type, error)
	Update(ctx context.Context, id string, in credtype.Input) (credtype.Type, error)
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
	types  TypeStore
	offers Offerer
	mailer mail.Mailer
}

func New(cfg Config, store Store, roles RoleStore, types TypeStore, offers Offerer, mailer mail.Mailer) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}

	return &Service{cfg: cfg, store: store, roles: roles, types: types, offers: offers, mailer: mailer}
}

// InvalidError is a request that cannot be recorded; its message says why.
type InvalidError struct{ Message string }

func (e InvalidError) Error() string { return e.Message }

// AlreadyIssuedError is a request for a credential of a type the person already has and that has not ended: it must be revoked first.
type AlreadyIssuedError struct{ IssuedAt time.Time }

func (e AlreadyIssuedError) Error() string {
	return "deze persoon heeft al een geldig credential van dit type (uitgegeven op " + e.IssuedAt.Format(
		"02-01-2006",
	) + "); dat moet eerst worden ingetrokken"
}

// AlreadyWaitingError is the same for a credential that is recorded but not yet fetched, with other data than the request.
type AlreadyWaitingError struct{}

func (AlreadyWaitingError) Error() string {
	return "voor deze persoon staat al een credential van dit type klaar met andere gegevens; dat moet eerst worden ingetrokken"
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

// RecordType records an entitlement to a credential of a type staff defined; values are the answers to its form, by field key.
func (s *Service) RecordType(ctx context.Context, def credential.Type, values map[string]string) (entitlement.Entitlement, error) {
	n, err := s.typeOf(def, values)
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

// typeOf checks the answers to the form of a type. A date field named validUntil ends the credential and must lie in the future.
func (s *Service) typeOf(def credential.Type, values map[string]string) (entitlement.New, error) {
	claims := map[string]string{}

	var end *time.Time

	for _, f := range def.Fields {
		v := strings.TrimSpace(values[f.Key])
		if v == "" {
			if f.Required {
				return entitlement.New{}, InvalidError{f.Key + " is required"}
			}

			continue
		}

		if len([]rune(v)) > credential.MaxValueSize {
			return entitlement.New{}, InvalidError{f.Key + " is too long"}
		}

		switch f.Kind {
		case credential.KindEmail:
			if !emailPattern.MatchString(v) {
				return entitlement.New{}, InvalidError{f.Key + " is not an email address"}
			}

			v = strings.ToLower(v)
		case credential.KindDate:
			if f.Key == credential.ValidUntilKey {
				e, day, err := s.endOf(v, f.Key)
				if err != nil {
					return entitlement.New{}, err
				}

				end, v = &e, day
			} else {
				d, err := credential.ParseDate(v)
				if err != nil {
					return entitlement.New{}, InvalidError{f.Key + " must be a date, dd-mm-yyyy or YYYY-MM-DD"}
				}

				v = d.Format(time.DateOnly)
			}
		case credential.KindNumber:
			n, err := credential.NormalizeNumber(f, v)
			if err != nil {
				return entitlement.New{}, InvalidError{err.Error()}
			}

			v = n
		case credential.KindChoice:
			if !contains(f.Options, v) {
				return entitlement.New{}, InvalidError{f.Key + " must be one of " + strings.Join(f.Options, ", ")}
			}
		}

		claims[f.Key] = v
	}

	return entitlement.New{Subject: entitlement.PersonOf(claims["email"]), Type: def.Type, Claims: claims, ValidUntil: end}, nil
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

	return entitlement.New{Subject: entitlement.PersonOf(in.Email), Type: credential.Employee, Claims: claims}, nil
}

// NotFoundError is a credential that does not exist or is no longer there to revoke.
type NotFoundError struct{}

func (NotFoundError) Error() string { return "dit credential bestaat niet of is al ingetrokken" }

// Replacement is the corrected data of a credential: the answers to the fields of its form, by key. Only the ones its type has are read.
// Roles are the roles of an Employee.
type Replacement struct {
	Values map[string]string
	Roles  []string
}

// correct builds the corrected entitlement of a credential from a Replacement.
type correct func(ctx context.Context, r Replacement) (entitlement.New, error)

// correction is how a credential of the type is corrected: an Employee has fixed fields, any other type has its form.
func (s *Service) correction(typeName string) correct {
	fixed := map[string]correct{
		credential.Employee: func(ctx context.Context, r Replacement) (entitlement.New, error) {
			v := r.Values

			return s.employeeOf(
				ctx,
				EmployeeInput{Name: v["name"], Email: v["email"], Department: v["department"], Organisation: v["organisation"], Roles: r.Roles},
			)
		},
	}
	if c, ok := fixed[typeName]; ok {
		return c
	}

	return func(ctx context.Context, r Replacement) (entitlement.New, error) {
		def, err := s.types.Find(ctx, typeName)
		if errors.Is(err, credtype.ErrNotFound) {
			return entitlement.New{}, fmt.Errorf("unknown credential type %q", typeName)
		}

		if err != nil {
			return entitlement.New{}, err
		}

		return s.typeOf(def, r.Values)
	}
}

// Active lists the credentials of the person that can be revoked: waiting to be fetched, or issued, and not ended.
func (s *Service) Active(ctx context.Context, email string) ([]entitlement.Entitlement, error) {
	if !emailPattern.MatchString(email) {
		return nil, InvalidError{"email is not an email address"}
	}

	return s.store.FindActiveFor(ctx, entitlement.PersonOf(email))
}

// AllActive lists the credentials of everyone that can be revoked, newest first.
func (s *Service) AllActive(ctx context.Context) ([]entitlement.Entitlement, error) {
	return s.store.FindActive(ctx)
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

	n, err := s.correction(old.Type)(ctx, r)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	// The email address is the person: another address is a credential for someone else, which is revoked and recorded anew, not corrected.
	if n.Subject != old.Subject {
		return entitlement.Entitlement{}, InvalidError{
			"het e-mailadres van een credential kan niet worden gewijzigd; trek het credential in en maak een nieuw credential aan",
		}
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
func (s *Service) endOf(value, field string) (time.Time, string, error) {
	d, err := credential.ParseDate(value)
	if err != nil {
		return time.Time{}, "", InvalidError{field + " must be a date, dd-mm-yyyy or YYYY-MM-DD"}
	}

	end := credential.EndOfDay(d)
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

// RenameRole changes the name of a role.
func (s *Service) RenameRole(ctx context.Context, id, name string) (role.Role, error) {
	return s.roles.Rename(ctx, id, name)
}

// DeleteRole refuses a role an employee holds (role.ErrInUse).
func (s *Service) DeleteRole(ctx context.Context, id string) error { return s.roles.Delete(ctx, id) }

// Types lists the credential types staff defined.
func (s *Service) Types(ctx context.Context) ([]credtype.Type, error) { return s.types.List(ctx) }

// TypeByPath finds the type whose form and entitlements live at the path (credtype.ErrNotFound).
func (s *Service) TypeByPath(ctx context.Context, path string) (credtype.Type, error) {
	return s.types.FindByPath(ctx, path)
}

// TypeTitle is what the wallet calls a credential of the type; the name of the type itself when it is unknown.
func (s *Service) TypeTitle(ctx context.Context, typeName string) string {
	if def, err := s.types.Find(ctx, typeName); err == nil {
		return def.Title
	}

	return typeName
}

func (s *Service) CreateType(ctx context.Context, in credtype.Input) (credtype.Type, error) {
	return s.types.Create(ctx, in)
}

// UpdateType refuses new fields for a type in use (credtype.ErrInUse).
func (s *Service) UpdateType(ctx context.Context, id string, in credtype.Input) (credtype.Type, error) {
	return s.types.Update(ctx, id, in)
}

// DeleteType refuses a type in use (credtype.ErrInUse).
func (s *Service) DeleteType(ctx context.Context, id string) error { return s.types.Delete(ctx, id) }

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}

// record makes the entitlement. A person has one of each credential at a time (one of each credential type): the same data again gives the
// entitlement that is waiting (and its QR code is mailed again), anything else is refused while one is waiting or issued and not ended.
func (s *Service) record(ctx context.Context, n entitlement.New) (entitlement.Entitlement, error) {
	if err := s.stands(ctx, n, ""); err != nil {
		return entitlement.Entitlement{}, err
	}

	return s.store.Create(ctx, n)
}

// stands reports the credential of the person's that stands in the way of n: one of the same type that is issued, or waiting with other data.
// The one with the id except does not count, and neither does one waiting with the same data, which Create gives back.
func (s *Service) stands(ctx context.Context, n entitlement.New, except string) error {
	open, err := s.store.FindOpenFor(ctx, n.Subject)
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

	issued, err := s.store.FindIssuedFor(ctx, n.Subject)
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
		To:      e.Subject.Email,
		Subject: "Uw credential staat klaar" + where,
		Text: fmt.Sprintf(
			"Er staat een credential voor u klaar%s.\n\n"+
				"Sla de bijlage (credential.png) op. Kies in uw wallet \"QR-code uploaden\" en kies dat bestand; de rest gaat vanzelf.\n\n"+
				"De QR-code is %d uur geldig en werkt één keer. Is hij verlopen? Dan stuurt de uitgever u automatisch een nieuwe.\n"+
				"Verwachtte u dit niet? Dan kunt u dit bericht negeren.\n",
			where,
			hours,
		),
		HTML: fmt.Sprintf(
			`<p>Er staat een credential voor u klaar%s.</p>`+
				`<p>Sla de afbeelding hieronder op. Kies in uw wallet <strong>QR-code uploaden</strong> `+
				`en kies dat bestand; de rest gaat vanzelf.</p>`+
				`<p><img src="cid:credential-qr" alt="QR-code" width="240" height="240"></p>`+
				`<p>De QR-code is %d uur geldig en werkt één keer. Is hij verlopen? Dan stuurt de uitgever u automatisch een nieuwe.</p>`+
				`<p>Verwachtte u dit niet? Dan kunt u dit bericht negeren.</p>`,
			htmlWhere,
			hours,
		),
		Image: &mail.Image{Filename: "credential.png", ContentID: "credential-qr", PNG: png},
	})
	if err != nil {
		return fmt.Errorf("mail the QR code: %w", err)
	}

	s.cfg.Log.Info(fmt.Sprintf("mailed a QR code for the %s credential to %s", e.Type, e.Subject.Email))

	return nil
}
