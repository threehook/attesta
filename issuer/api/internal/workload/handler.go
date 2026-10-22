package workload

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"attesta/issuer/internal/svid"
)

type workloadJSON struct {
	ID          string    `json:"id"`
	SpiffeID    string    `json:"spiffeId"`
	Owner       string    `json:"owner"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"createdAt"`
}

// offersJSON: offer is the link for the Workload credential, mandate the one for the Mandate credential, left out when none is active.
type offersJSON struct {
	Offer   string `json:"offer"`
	Mandate string `json:"mandate,omitempty"`
}

func jsonOf(w Workload) workloadJSON {
	return workloadJSON{ID: w.ID, SpiffeID: w.SpiffeID, Owner: w.Owner, Environment: w.Environment, CreatedAt: w.CreatedAt}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// Handler serves the workloads' own endpoint, which they reach with a JWT-SVID, and the endpoints staff enroll them with, which protect wraps.
// A nil protect leaves staff's endpoints open.
func Handler(svc *Service, protect func(http.Handler) http.Handler) http.Handler {
	if protect == nil {
		protect = func(h http.Handler) http.Handler { return h }
	}
	staff := http.NewServeMux()
	staff.HandleFunc("POST /workloads", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			SpiffeID    string `json:"spiffeId"`
			Owner       string `json:"owner"`
			Environment string `json:"environment"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "the request is not JSON")
			return
		}
		enrolled, err := svc.Enroll(r.Context(), Input{SpiffeID: in.SpiffeID, Owner: in.Owner, Environment: in.Environment})
		switch {
		case errors.Is(err, ErrInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrExists):
			writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			slog.Error("could not enroll the workload", "error", err)
			writeError(w, http.StatusInternalServerError, "the workload could not be enrolled")
		default:
			writeJSON(w, http.StatusCreated, jsonOf(enrolled))
		}
	})
	staff.HandleFunc("GET /workloads", func(w http.ResponseWriter, r *http.Request) {
		list, err := svc.List(r.Context())
		if err != nil {
			slog.Error("could not read the workloads", "error", err)
			writeError(w, http.StatusInternalServerError, "the workloads could not be read")
			return
		}
		out := make([]workloadJSON, len(list))
		for i, item := range list {
			out[i] = jsonOf(item)
		}
		writeJSON(w, http.StatusOK, out)
	})
	staff.HandleFunc("DELETE /workloads/{id}", func(w http.ResponseWriter, r *http.Request) {
		err := svc.Revoke(r.Context(), r.PathValue("id"))
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case err != nil:
			slog.Error("could not revoke the workload", "error", err)
			writeError(w, http.StatusInternalServerError, "the workload could not be revoked")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	mux := http.NewServeMux()
	mux.Handle("/workloads", protect(staff))
	mux.Handle("/workloads/", protect(staff))
	mux.HandleFunc("POST /workloads/offer", func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "a JWT-SVID is required")
			return
		}
		offers, err := svc.Offer(r.Context(), token)
		switch {
		case errors.Is(err, svid.ErrInvalid):
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			writeError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrNotEnrolled):
			writeError(w, http.StatusForbidden, err.Error())
		case err != nil:
			slog.Error("could not make an offer for a workload", "error", err)
			writeError(w, http.StatusInternalServerError, "the offer could not be made")
		default:
			writeJSON(w, http.StatusOK, offersJSON{Offer: offers.Workload, Mandate: offers.Mandate})
		}
	})
	return mux
}
