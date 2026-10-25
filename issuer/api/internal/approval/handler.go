package approval

import (
	"attesta/issuer/internal/svid"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

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
	case errors.Is(err, svid.ErrInvalid):
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrNotEnrolled):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrGone):
		writeError(w, http.StatusGone, err.Error())
	default:
		slog.Error("could not "+what, "error", err)
		writeError(w, http.StatusInternalServerError, "the request could not be "+what)
	}
}

// forWorkload is what the workload that asked is told.
type forWorkload struct {
	ID        string `json:"id"`
	Status    State  `json:"status"`
	Approvals int    `json:"approvals"`
	Required  int    `json:"required"`
}

type forPeople struct {
	ID           string            `json:"id"`
	Workload     string            `json:"workload"`
	Resource     string            `json:"resource"`
	Action       map[string]string `json:"action"`
	CreatedAt    time.Time         `json:"createdAt"`
	ExpiresAt    time.Time         `json:"expiresAt"`
	Approvers    []string          `json:"approvers"`
	Required     int               `json:"required"`
	ApprovedByMe bool              `json:"approvedByMe"`
	// ValidSeconds is how long the Approval credential lasts after the last approval.
	ValidSeconds int64 `json:"validSeconds"`
}

func bearer(w http.ResponseWriter, r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "a JWT-SVID is required")
	}

	return token, ok && token != ""
}

// WorkloadHandler serves the endpoints a workload parks an action with and takes its credential from; it shows a JWT-SVID. It is mounted on
// /workloads/approvals.
func WorkloadHandler(svc *Service) http.Handler {
	told := func(r Request) forWorkload {
		return forWorkload{ID: r.ID, Status: r.State, Approvals: len(r.Approvers), Required: svc.Required()}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /workloads/approvals", func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(w, r)
		if !ok {
			return
		}

		var in struct {
			Resource string            `json:"resource"`
			Action   map[string]string `json:"action"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "the request is not JSON")
			return
		}

		parked, err := svc.Request(r.Context(), token, in.Resource, in.Action)
		if err != nil {
			fail(w, err, "parked")
			return
		}

		writeJSON(w, http.StatusOK, told(parked))
	})
	mux.HandleFunc("GET /workloads/approvals/{id}", func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(w, r)
		if !ok {
			return
		}

		got, err := svc.Pending(r.Context(), token, r.PathValue("id"))
		if err != nil {
			fail(w, err, "read")
			return
		}

		writeJSON(w, http.StatusOK, told(got))
	})
	mux.HandleFunc("POST /workloads/approvals/{id}/offer", func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(w, r)
		if !ok {
			return
		}

		link, err := svc.Offer(r.Context(), token, r.PathValue("id"))
		if errors.Is(err, ErrNotApproved) {
			got, _ := svc.Pending(r.Context(), token, r.PathValue("id"))
			writeJSON(w, http.StatusAccepted, told(got))

			return
		}

		if err != nil {
			fail(w, err, "handed out")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"offer": link})
	})

	return mux
}

// PeopleHandler serves the endpoints people approve or reject requests with. protect lets in only people who may approve, and approver says who the
// person is.
func PeopleHandler(svc *Service, protect func(http.Handler) http.Handler, approver func(*http.Request) string) http.Handler {
	if protect == nil {
		protect = func(h http.Handler) http.Handler { return h }
	}

	told := func(r Request, me string) forPeople {
		return forPeople{
			ID: r.ID, Workload: r.Workload, Resource: r.Resource, Action: r.Action, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
			Approvers: r.Approvers,
			Required:  svc.Required(), ApprovedByMe: me != "" && slices.Contains(r.Approvers, me),
			ValidSeconds: int64(svc.CredentialLifetime().Seconds()),
		}
	}
	people := http.NewServeMux()
	people.HandleFunc("GET /approvals", func(w http.ResponseWriter, r *http.Request) {
		list, err := svc.List(r.Context())
		if err != nil {
			fail(w, err, "read")
			return
		}

		me := approver(r)

		out := make([]forPeople, len(list))
		for i, item := range list {
			out[i] = told(item, me)
		}

		writeJSON(w, http.StatusOK, out)
	})
	people.HandleFunc("POST /approvals/{id}/approvals", func(w http.ResponseWriter, r *http.Request) {
		email := approver(r)
		if email == "" {
			writeError(w, http.StatusUnauthorized, "inloggen is nodig")
			return
		}

		got, err := svc.Approve(r.Context(), r.PathValue("id"), email)
		if err != nil {
			fail(w, err, "approved")
			return
		}

		writeJSON(w, http.StatusOK, told(got, email))
	})
	people.HandleFunc("DELETE /approvals/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Reject(r.Context(), r.PathValue("id")); err != nil {
			fail(w, err, "rejected")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	mux := http.NewServeMux()
	mux.Handle("/approvals", protect(people))
	mux.Handle("/approvals/", protect(people))

	return mux
}
