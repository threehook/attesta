// Package httpapi is the JSON API the issuer's pages call: what to show in the forms, and recording an entitlement.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/service"
)

type Service interface {
	RecordDiploma(ctx context.Context, d credential.Diploma, in service.DiplomaInput) (entitlement.Entitlement, error)
	RecordEmployee(ctx context.Context, in service.EmployeeInput) (entitlement.Entitlement, error)
	MailQRCode(ctx context.Context, e entitlement.Entitlement) error
	Roles(ctx context.Context) ([]string, error)
}

type Config struct {
	IssuerName string
	// DID is the issuer as verifiers know it.
	DID string
	// Protocol serves the OpenID4VCI endpoints for wallets.
	Protocol http.Handler
	// Protect wraps what only staff may use; nil leaves it open.
	Protect func(http.Handler) http.Handler
}

// Handler serves the API and, next to it, the endpoints wallets use.
func Handler(cfg Config, svc Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", cfg.Protocol)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	staff := http.NewServeMux()
	protect := cfg.Protect
	if protect == nil {
		protect = func(h http.Handler) http.Handler { return h }
	}
	mux.Handle("/info", protect(staff))
	mux.Handle("/entitlements/", protect(staff))
	staff.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		roles, err := svc.Roles(r.Context())
		if err != nil {
			slog.Error("could not read the roles", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "de rollen konden niet worden gelezen"})
			return
		}
		diplomas := make([]map[string]string, len(credential.Diplomas))
		for i, d := range credential.Diplomas {
			diplomas[i] = map[string]string{"path": d.Path, "title": d.Title, "description": d.Description}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"issuerName": cfg.IssuerName, "did": cfg.DID, "departments": credential.Departments, "roles": roles, "diplomas": diplomas,
		})
	})
	staff.HandleFunc("POST /entitlements/diploma/{path}", func(w http.ResponseWriter, r *http.Request) {
		diploma, ok := credential.DiplomaByPath(r.PathValue("path"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "onbekend diploma"})
			return
		}
		var in struct {
			Name, Email, ValidUntil string
		}
		if !decode(w, r, &in) {
			return
		}
		e, err := svc.RecordDiploma(r.Context(), diploma, service.DiplomaInput{Name: in.Name, Email: in.Email, ValidUntil: in.ValidUntil})
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
	return mux
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
