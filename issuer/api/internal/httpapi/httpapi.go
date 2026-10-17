// Package httpapi is the JSON API the issuer's pages call: what to show in the forms, and recording an entitlement.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/credtype"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/role"
	"attesta/issuer/internal/service"
	"attesta/issuer/internal/statuslist"
)

type Service interface {
	RecordType(ctx context.Context, def credential.Type, values map[string]string) (entitlement.Entitlement, error)
	RecordEmployee(ctx context.Context, in service.EmployeeInput) (entitlement.Entitlement, error)
	MailQRCode(ctx context.Context, e entitlement.Entitlement) error
	Roles(ctx context.Context) ([]string, error)
	ListRoles(ctx context.Context) ([]role.Role, error)
	CreateRole(ctx context.Context, name string) (role.Role, error)
	RenameRole(ctx context.Context, id, name string) (role.Role, error)
	DeleteRole(ctx context.Context, id string) error
	Types(ctx context.Context) ([]credtype.Type, error)
	TypeByPath(ctx context.Context, path string) (credtype.Type, error)
	TypeTitle(ctx context.Context, typeName string) string
	CreateType(ctx context.Context, in credtype.Input) (credtype.Type, error)
	UpdateType(ctx context.Context, id string, in credtype.Input) (credtype.Type, error)
	DeleteType(ctx context.Context, id string) error
	Active(ctx context.Context, email string) ([]entitlement.Entitlement, error)
	AllActive(ctx context.Context) ([]entitlement.Entitlement, error)
	Revoke(ctx context.Context, id string) error
	Replace(ctx context.Context, id string, r service.Replacement) (entitlement.Entitlement, error)
}

type Config struct {
	IssuerName string
	// DID is the issuer as verifiers know it.
	DID string
	// Protocol serves the OpenID4VCI endpoints for wallets.
	Protocol http.Handler
	// Status serves the status list; nil leaves it out.
	Status http.Handler
	// Protect wraps what only staff may use; nil leaves it open.
	Protect func(http.Handler) http.Handler
}

// Handler serves the API and, next to it, the endpoints wallets use.
func Handler(cfg Config, svc Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", cfg.Protocol)
	if cfg.Status != nil {
		mux.Handle(statuslist.Path, cfg.Status)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	staff := http.NewServeMux()
	protect := cfg.Protect
	if protect == nil {
		protect = func(h http.Handler) http.Handler { return h }
	}
	mux.Handle("/info", protect(staff))
	mux.Handle("/entitlements", protect(staff))
	mux.Handle("/entitlements/", protect(staff))
	mux.Handle("/roles", protect(staff))
	mux.Handle("/roles/", protect(staff))
	mux.Handle("/credential-types", protect(staff))
	mux.Handle("/credential-types/", protect(staff))
	staff.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		roles, err := svc.Roles(r.Context())
		if err != nil {
			slog.Error("could not read the roles", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "de rollen konden niet worden gelezen"})
			return
		}
		types, err := svc.Types(r.Context())
		if err != nil {
			slog.Error("could not read the credential types", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "de credentialtypes konden niet worden gelezen"})
			return
		}
		list := make([]map[string]any, len(types))
		for i, t := range types {
			list[i] = map[string]any{"type": t.Type.Type, "path": t.Path, "title": t.Title, "description": t.Description, "fields": t.Fields}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"issuerName": cfg.IssuerName, "did": cfg.DID, "departments": credential.Departments, "roles": roles, "types": list,
		})
	})
	staff.HandleFunc("POST /entitlements/type/{path}", func(w http.ResponseWriter, r *http.Request) {
		def, err := svc.TypeByPath(r.Context(), r.PathValue("path"))
		if errors.Is(err, credtype.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "onbekend credentialtype"})
			return
		}
		if err != nil {
			slog.Error("could not read the credential type", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "het credentialtype kon niet worden gelezen"})
			return
		}
		var in map[string]string
		if !decode(w, r, &in) {
			return
		}
		e, err := svc.RecordType(r.Context(), def.Type, in)
		grant(w, r, svc, e, err)
	})
	staff.HandleFunc("POST /entitlements/employee", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name, Email, Department, Organisation string
			Roles                                 []string
		}
		if !decode(w, r, &in) {
			return
		}
		e, err := svc.RecordEmployee(r.Context(), service.EmployeeInput{
			Name: in.Name, Email: in.Email, Department: in.Department, Organisation: in.Organisation, Roles: in.Roles,
		})
		grant(w, r, svc, e, err)
	})
	staff.HandleFunc("GET /entitlements", func(w http.ResponseWriter, r *http.Request) {
		// Without an email address this lists the credentials of everyone.
		var active []entitlement.Entitlement
		var err error
		if r.URL.Query().Has("email") {
			active, err = svc.Active(r.Context(), r.URL.Query().Get("email"))
		} else {
			active, err = svc.AllActive(r.Context())
		}
		var invalid service.InvalidError
		if errors.As(err, &invalid) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": invalid.Message})
			return
		}
		if err != nil {
			slog.Error("could not read the entitlements", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "de credentials konden niet worden gelezen"})
			return
		}
		out := make([]map[string]any, len(active))
		for i, e := range active {
			status := "waiting"
			if e.Status == entitlement.Issued {
				status = "issued"
			}
			out[i] = map[string]any{
				"id": e.ID, "type": e.Type, "title": svc.TypeTitle(r.Context(), e.Type), "status": status, "claims": e.Claims,
				"validUntil": e.ValidUntil, "createdAt": e.CreatedAt, "issuedAt": e.IssuedAt,
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entitlements": out})
	})
	staff.HandleFunc("GET /roles", func(w http.ResponseWriter, r *http.Request) {
		roles, err := svc.ListRoles(r.Context())
		if err != nil {
			slog.Error("could not read the roles", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "de rollen konden niet worden gelezen"})
			return
		}
		out := make([]map[string]any, len(roles))
		for i, ro := range roles {
			out[i] = roleJSON(ro)
		}
		writeJSON(w, http.StatusOK, map[string]any{"roles": out})
	})
	staff.HandleFunc("POST /roles", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !decode(w, r, &in) {
			return
		}
		ro, err := svc.CreateRole(r.Context(), in.Name)
		if err != nil {
			refuseRole(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, roleJSON(ro))
	})
	staff.HandleFunc("PUT /roles/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !decode(w, r, &in) {
			return
		}
		ro, err := svc.RenameRole(r.Context(), r.PathValue("id"), in.Name)
		if err != nil {
			refuseRole(w, err)
			return
		}
		writeJSON(w, http.StatusOK, roleJSON(ro))
	})
	staff.HandleFunc("DELETE /roles/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := svc.DeleteRole(r.Context(), r.PathValue("id")); err != nil {
			refuseRole(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	typeInput := func(w http.ResponseWriter, r *http.Request) (credtype.Input, bool) {
		var in struct {
			Title, Description string
			Fields             []credential.Field
		}
		if !decode(w, r, &in) {
			return credtype.Input{}, false
		}
		return credtype.Input{Title: in.Title, Description: in.Description, Fields: in.Fields}, true
	}
	staff.HandleFunc("GET /credential-types", func(w http.ResponseWriter, r *http.Request) {
		types, err := svc.Types(r.Context())
		if err != nil {
			refuseType(w, err)
			return
		}
		out := make([]map[string]any, len(types))
		for i, t := range types {
			out[i] = typeJSON(t)
		}
		writeJSON(w, http.StatusOK, map[string]any{"types": out})
	})
	staff.HandleFunc("POST /credential-types", func(w http.ResponseWriter, r *http.Request) {
		in, ok := typeInput(w, r)
		if !ok {
			return
		}
		t, err := svc.CreateType(r.Context(), in)
		if err != nil {
			refuseType(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, typeJSON(t))
	})
	staff.HandleFunc("PUT /credential-types/{id}", func(w http.ResponseWriter, r *http.Request) {
		in, ok := typeInput(w, r)
		if !ok {
			return
		}
		t, err := svc.UpdateType(r.Context(), r.PathValue("id"), in)
		if err != nil {
			refuseType(w, err)
			return
		}
		writeJSON(w, http.StatusOK, typeJSON(t))
	})
	staff.HandleFunc("DELETE /credential-types/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := svc.DeleteType(r.Context(), r.PathValue("id")); err != nil {
			refuseType(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Without a body this revokes; with the corrected data of the credential it revokes and records the corrected one, and mails its QR code.
	staff.HandleFunc("POST /entitlements/revoke/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var raw map[string]any
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		replace := true
		if err := json.NewDecoder(r.Body).Decode(&raw); errors.Is(err, io.EOF) {
			replace = false
		} else if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request is not the JSON this endpoint takes"})
			return
		}
		if !replace {
			if err := svc.Revoke(r.Context(), id); err != nil {
				refuse(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
			return
		}
		replacement, ok := replacementOf(raw)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request is not the JSON this endpoint takes"})
			return
		}
		e, err := svc.Replace(r.Context(), id, replacement)
		if err != nil {
			refuse(w, err)
			return
		}
		grant(w, r, svc, e, nil)
	})
	return mux
}

// replacementOf reads the corrected data: text per field and, for an Employee, a list of roles.
func replacementOf(raw map[string]any) (service.Replacement, bool) {
	r := service.Replacement{Values: map[string]string{}}
	for k, v := range raw {
		switch v := v.(type) {
		case string:
			r.Values[k] = v
		case []any:
			if k != "roles" {
				return r, false
			}
			for _, item := range v {
				name, ok := item.(string)
				if !ok {
					return r, false
				}
				r.Roles = append(r.Roles, name)
			}
		default:
			return r, false
		}
	}
	return r, true
}

func typeJSON(t credtype.Type) map[string]any {
	return map[string]any{
		"id": t.ID, "type": t.Type.Type, "path": t.Path, "title": t.Title, "description": t.Description, "fields": t.Fields, "inUse": t.InUse,
		"createdAt": t.CreatedAt, "updatedAt": t.UpdatedAt,
	}
}

// refuseType answers a request to change a credential type that could not be done.
func refuseType(w http.ResponseWriter, err error) {
	var invalid credtype.InvalidError
	switch {
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": invalid.Message})
	case errors.Is(err, credtype.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, credtype.ErrExists), errors.Is(err, credtype.ErrInUse):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		slog.Error("could not change the credential type", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "het credentialtype kon niet worden gewijzigd"})
	}
}

func roleJSON(r role.Role) map[string]any {
	return map[string]any{"id": r.ID, "name": r.Name, "inUse": r.InUse, "createdAt": r.CreatedAt, "updatedAt": r.UpdatedAt}
}

// refuseRole answers a request to change a role that could not be done.
func refuseRole(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, role.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, role.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, role.ErrExists), errors.Is(err, role.ErrInUse):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		slog.Error("could not change the role", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "de rol kon niet worden gewijzigd"})
	}
}

// refuse answers a request to revoke that could not be done.
func refuse(w http.ResponseWriter, err error) {
	var invalid service.InvalidError
	var missing service.NotFoundError
	var issued service.AlreadyIssuedError
	var waiting service.AlreadyWaitingError
	switch {
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": invalid.Message})
	case errors.As(err, &missing):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.As(err, &issued), errors.As(err, &waiting):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		slog.Error("could not revoke the credential", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "het credential kon niet worden ingetrokken"})
	}
}

// grant answers a request to record an entitlement: the person gets the mail with the QR code.
func grant(w http.ResponseWriter, r *http.Request, svc Service, e entitlement.Entitlement, err error) {
	var invalid service.InvalidError
	if errors.As(err, &invalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": invalid.Message})
		return
	}
	var issued service.AlreadyIssuedError
	var waiting service.AlreadyWaitingError
	if errors.As(err, &issued) || errors.As(err, &waiting) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		slog.Error("could not record the entitlement", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "het credential kon niet worden vastgelegd"})
		return
	}
	if err := svc.MailQRCode(r.Context(), e); err != nil {
		slog.Error("could not mail the QR code", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "het credential is vastgelegd, maar de e-mail kon niet worden verstuurd; probeer het opnieuw", "entitlementId": e.ID,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"entitlementId": e.ID, "mailedTo": e.Email})
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(into); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request is not the JSON this endpoint takes"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
