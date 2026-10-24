package mandate

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

type mandateJSON struct {
	ID        string    `json:"id"`
	Workload  string    `json:"workload"`
	Resource  string    `json:"resource"`
	ExpiresAt time.Time `json:"expiresAt"`
	Approvers []string  `json:"approvers"`
	CreatedAt time.Time `json:"createdAt"`
	// ApprovedByMe is whether the person who asks is among the approvers, so a page can leave out approving again.
	ApprovedByMe bool `json:"approvedByMe"`
}

func jsonOf(m Mandate, me string) mandateJSON {
	return mandateJSON{
		ID: m.ID, Workload: m.Workload, Resource: m.Resource, ExpiresAt: m.ExpiresAt, Approvers: m.Approvers, CreatedAt: m.CreatedAt,
		ApprovedByMe: me != "" && slices.Contains(m.Approvers, me),
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// fail answers for an error of the service; what is not one of ours is a server error.
func fail(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrNotEnrolled):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("could not "+what, "error", err)
		writeError(w, http.StatusInternalServerError, "the mandate could not be "+what)
	}
}

// Handler serves the endpoints people approve mandates with. protect lets in only people who may approve, and approver says who the person is.
func Handler(svc *Service, protect func(http.Handler) http.Handler, approver func(*http.Request) string) http.Handler {
	if protect == nil {
		protect = func(h http.Handler) http.Handler { return h }
	}

	people := http.NewServeMux()
	// who answers 401 for a request whose person cannot be told.
	who := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		email := approver(r)
		if email == "" {
			writeError(w, http.StatusUnauthorized, "inloggen is nodig")
		}

		return email, email != ""
	}
	people.HandleFunc("POST /mandates", func(w http.ResponseWriter, r *http.Request) {
		email, ok := who(w, r)
		if !ok {
			return
		}

		var in struct {
			Workload  string    `json:"workload"`
			Resource  string    `json:"resource"`
			ExpiresAt time.Time `json:"expiresAt"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "the request is not JSON")
			return
		}

		m, err := svc.Create(r.Context(), email, Input{Workload: in.Workload, Resource: in.Resource, ExpiresAt: in.ExpiresAt})
		if err != nil {
			fail(w, err, "approved")
			return
		}

		writeJSON(w, http.StatusCreated, jsonOf(m, email))
	})
	people.HandleFunc("GET /mandates", func(w http.ResponseWriter, r *http.Request) {
		list, err := svc.List(r.Context())
		if err != nil {
			fail(w, err, "read")
			return
		}

		me := approver(r)

		out := make([]mandateJSON, len(list))
		for i, m := range list {
			out[i] = jsonOf(m, me)
		}

		writeJSON(w, http.StatusOK, out)
	})
	people.HandleFunc("POST /mandates/{id}/approvals", func(w http.ResponseWriter, r *http.Request) {
		email, ok := who(w, r)
		if !ok {
			return
		}

		m, err := svc.Approve(r.Context(), r.PathValue("id"), email)
		if err != nil {
			fail(w, err, "approved")
			return
		}

		writeJSON(w, http.StatusOK, jsonOf(m, email))
	})
	people.HandleFunc("DELETE /mandates/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := svc.End(r.Context(), r.PathValue("id")); err != nil {
			fail(w, err, "ended")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	mux := http.NewServeMux()
	mux.Handle("/mandates", protect(people))
	mux.Handle("/mandates/", protect(people))

	return mux
}
