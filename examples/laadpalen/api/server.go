package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What this app asks attesta. Signing in asks for the employee credential and nothing beyond the email that identifies them; a laadpaal request asks
// for the claims its policy reads.
const (
	signInResource = "sign_in"
	signInPolicyID = "sign_in"
	resource       = "request_laadpaal"
	policyID       = "request_laadpaal"
	credentialType = "Employee"
	// submissionTTL is how long a submission is remembered; attesta forgets a request after five minutes, so a longer one only keeps finished results.
	submissionTTL = 30 * time.Minute
	// sessionTTL is how long an employee who signed in with the wallet stays signed in. It runs from the sign-in and is not extended by use.
	sessionTTL    = 30 * time.Minute
	sessionCookie = "laadpalen_session"
)

var claimsAsked = []string{"department", "diploma"}

var postcodePattern = regexp.MustCompile(`^[0-9]{4}[A-Z]{2}$`)

// submission is a sign-in or a laadpaal request that was handed to attesta.
type submission struct {
	signIn bool
	// by is the signed-in employee a laadpaal request was made for; the credential shown for it must be theirs.
	by          subject
	postcode    string
	houseNumber int
	created     time.Time
	// answered is set once attesta has decided (or forgotten the request); from then on view is the answer and attesta is not asked again.
	answered bool
	view     submissionView
}

// session is an employee who signed in with the wallet; a laadpaal request can only be made in one.
type session struct {
	subject subject
	expires time.Time
}

type submissionView struct {
	Status string `json:"status"` // pending, done or expired
	// Authorized is attesta's decision on the employee; Reason is why. Only an authorized employee gets a Result.
	Authorized bool     `json:"authorized"`
	Reason     string   `json:"reason,omitempty"`
	Subject    *subject `json:"subject,omitempty"`
	Result     *result  `json:"result,omitempty"`
	// Debug shows the page what this app exchanged with attesta.
	Debug debugView `json:"debug"`
}

type debugView struct {
	AuthorizationRequest string          `json:"authorizationRequest"`
	Outcome              json.RawMessage `json:"outcome,omitempty"`
}

// recorded is a request that was authorized and decided: who submitted it, for which address, and what came of it.
type recorded struct {
	Subject     subject
	Postcode    string
	HouseNumber int
	Result      result
}

type server struct {
	attesta authorizer
	logger  *slog.Logger
	now     func() time.Time
	// application is the origin wallets know this app by (where they answer attesta); the page gives it to the wallet to ask about this app.
	application string
	// userRoles gives an employee, by email, the roles this app sends attesta with a laadpaal request. An employee not listed has none.
	userRoles map[string][]string

	mu          sync.Mutex
	submissions map[string]*submission
	sessions    map[string]session
	records     []recorded
}

func newServer(attesta authorizer, logger *slog.Logger) *server {
	return &server{attesta: attesta, logger: logger, now: time.Now, submissions: make(map[string]*submission), sessions: make(map[string]session)}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/sign-in", s.handleSignIn)
	mux.HandleFunc("GET /api/sign-in/{id}", s.handleSignInStatus)
	mux.HandleFunc("POST /api/request-laadpaal", s.handleSubmit)
	mux.HandleFunc("GET /api/request-laadpaal/{id}", s.handleStatus)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("DELETE /api/session", s.handleSignOut)
	return mux
}

type submitRequest struct {
	Postcode    string `json:"postcode"`
	HouseNumber string `json:"huisnummer"`
}

type submitResponse struct {
	RequestID string `json:"requestId"`
	// Link is what the employee opens in their wallet.
	Link string `json:"authorizationRequest"`
}

type configView struct {
	Application string `json:"application"`
}

func (s *server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, configView{Application: s.application})
}

// handleSignIn starts a sign-in: attesta gets a link for the employee's wallet, which shows the employee credential's identity and nothing else.
func (s *server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	started, err := s.attesta.start(r.Context(), signInResource, signInPolicyID, credentialType, nil, nil)
	if err != nil {
		s.logger.Error("starting the sign-in failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "kon het aanmelden niet starten"})
		return
	}
	s.mu.Lock()
	s.forget()
	s.submissions[started.RequestID] = &submission{
		signIn: true, created: s.now(), view: submissionView{Status: "pending", Debug: debugView{AuthorizationRequest: started.Link}},
	}
	s.mu.Unlock()

	s.logger.Info("sign-in started", "requestId", started.RequestID, "traceId", started.TraceID)
	writeJSON(w, http.StatusOK, submitResponse{RequestID: started.RequestID, Link: started.Link})
}

// handleSubmit starts a laadpaal request for a signed-in employee. What the employee may submit is attesta's decision, so this only checks the input
// and asks attesta for the wallet link.
func (s *server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sess, signedIn := s.currentSession(r)
	s.mu.Unlock()
	if !signedIn {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "u bent niet aangemeld"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var in submitRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ongeldige aanvraag"})
		return
	}
	postcode := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(in.Postcode), " ", ""))
	houseNumber, err := strconv.Atoi(strings.TrimSpace(in.HouseNumber))
	if !postcodePattern.MatchString(postcode) || err != nil || houseNumber < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ongeldige postcode of huisnummer"})
		return
	}

	started, err := s.attesta.start(r.Context(), resource, policyID, credentialType, claimsAsked, s.userRoles[strings.ToLower(sess.subject.Email)])
	if err != nil {
		s.logger.Error("starting the authorization failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "kon de autorisatie niet starten"})
		return
	}

	s.mu.Lock()
	s.forget()
	s.submissions[started.RequestID] = &submission{
		by: sess.subject, postcode: postcode, houseNumber: houseNumber, created: s.now(),
		view: submissionView{Status: "pending", Debug: debugView{AuthorizationRequest: started.Link}},
	}
	s.mu.Unlock()

	s.logger.Info("submission started", "requestId", started.RequestID, "traceId", started.TraceID, "email", sess.subject.Email, "postcode", postcode, "houseNumber", houseNumber)
	writeJSON(w, http.StatusOK, submitResponse{RequestID: started.RequestID, Link: started.Link})
}

func (s *server) handleSignInStatus(w http.ResponseWriter, r *http.Request) { s.status(w, r, true) }

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) { s.status(w, r, false) }

// status reports on a sign-in or a submission. Once the wallet has answered and attesta has decided, an authorized sign-in starts the session and an
// authorized submission is carried out exactly once: the address rules run, and the outcome is recorded with the employee that submitted it.
func (s *server) status(w http.ResponseWriter, r *http.Request, signIn bool) {
	id := r.PathValue("id")

	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.submissions[id]
	if !ok || sub.signIn != signIn {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "onbekende aanvraag"})
		return
	}
	if !sub.answered {
		if err := s.settle(r.Context(), id, sub); err != nil {
			s.logger.Error("reading the decision failed", "requestId", id, "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "kon de beslissing niet ophalen"})
			return
		}
		if signIn && sub.answered && sub.view.Authorized && sub.view.Subject != nil {
			s.startSession(w, r, *sub.view.Subject)
		}
	}
	writeJSON(w, http.StatusOK, sub.view)
}

// currentSession returns the employee's session if the request carries a valid one. Callers hold s.mu.
func (s *server) currentSession(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	sess, ok := s.sessions[c.Value]
	if !ok || !s.now().Before(sess.expires) {
		delete(s.sessions, c.Value)
		return session{}, false
	}
	return sess, true
}

// startSession remembers an employee that attesta authorized and hands the browser the cookie that names the session. Callers hold s.mu.
func (s *server) startSession(w http.ResponseWriter, r *http.Request, who subject) {
	token := newToken()
	expires := s.now().Add(sessionTTL)
	s.sessions[token] = session{subject: who, expires: expires}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	s.logger.Info("session started", "email", who.Email, "issuer", who.Issuer, "expires", expires)
}

type sessionView struct {
	Active    bool      `json:"active"`
	Subject   *subject  `json:"subject,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}

// handleSession tells the page who is signed in.
func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.currentSession(r)
	if !ok {
		writeJSON(w, http.StatusOK, sessionView{})
		return
	}
	writeJSON(w, http.StatusOK, sessionView{Active: true, Subject: &sess.subject, ExpiresAt: sess.expires})
}

// handleSignOut ends the session, so the employee has to sign in again.
func (s *server) handleSignOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// settle asks attesta for the decision on a submission and, when there is one, finishes the submission. Callers hold s.mu.
func (s *server) settle(ctx context.Context, id string, sub *submission) error {
	got, raw, err := s.attesta.outcome(ctx, id)
	if errors.Is(err, errUnknownRequest) {
		sub.answered = true
		sub.view.Status = "expired"
		sub.view.Reason = "De aanvraag is verlopen; begin opnieuw"
		return nil
	}
	if err != nil {
		return err
	}
	if got.Status != "done" {
		return nil
	}

	sub.answered = true
	sub.view.Status = "done"
	sub.view.Authorized = got.Allow
	sub.view.Reason = got.Reason
	sub.view.Debug.Outcome = raw
	if !got.Allow || got.Subject == nil {
		sub.view.Authorized = false
		s.logger.Info("submission not authorized", "requestId", id, "reason", got.Reason)
		return nil
	}

	if sub.signIn {
		sub.view.Subject = got.Subject
		s.logger.Info("sign-in decided", "requestId", id, "email", got.Subject.Email, "issuer", got.Subject.Issuer)
		return nil
	}
	if *got.Subject != sub.by {
		sub.view.Authorized = false
		sub.view.Reason = "Het bewijs hoort niet bij de aangemelde medewerker"
		s.logger.Info("submission not authorized", "requestId", id, "reason", sub.view.Reason, "email", got.Subject.Email, "signedInAs", sub.by.Email)
		return nil
	}

	decided := decideRequest(sub.postcode, sub.houseNumber)
	sub.view.Subject = got.Subject
	sub.view.Result = &decided
	s.records = append(s.records, recorded{Subject: *got.Subject, Postcode: sub.postcode, HouseNumber: sub.houseNumber, Result: decided})
	s.logger.Info("submission decided", "requestId", id, "email", got.Subject.Email, "issuer", got.Subject.Issuer,
		"postcode", sub.postcode, "houseNumber", sub.houseNumber, "granted", decided.Granted, "reason", decided.Reason)
	return nil
}

// forget drops submissions that are older than submissionTTL and sessions that have expired. Callers hold s.mu.
func (s *server) forget() {
	for id, sub := range s.submissions {
		if s.now().Sub(sub.created) > submissionTTL {
			delete(s.submissions, id)
		}
	}
	for token, sess := range s.sessions {
		if !s.now().Before(sess.expires) {
			delete(s.sessions, token)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
