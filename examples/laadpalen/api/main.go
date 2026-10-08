// Command laadpalen-api is the backend of the laadpalen example: municipality employees submit requests for a laadpaal (EV charging point). The page
// calls this server and nothing else; this server asks attesta, running as a sidecar in the same pod, whether the employee may submit, and then
// carries the request out.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "laadpalen-api:", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := getenv("LAADPALEN_ADDR", ":8787")
	// The attesta sidecar in the same pod.
	attestaURL := getenv("ATTESTA_URL", "http://127.0.0.1:8080")

	app := newServer(newAttestaClient(attestaURL), logger)
	// Where wallets answer attesta; wallets know the application by this origin.
	if public, err := url.Parse(os.Getenv("ATTESTA_PUBLIC_URL")); err == nil && public.Scheme != "" && public.Host != "" {
		app.application = public.Scheme + "://" + public.Host
	}

	app.userRoles = parseUserRoles(os.Getenv("LAADPALEN_USER_ROLES"))

	srv := &http.Server{
		Addr:              addr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("listening", "addr", addr, "attesta", attestaURL)
	return srv.ListenAndServe()
}

// parseUserRoles reads "ada@example.com=role1,role2;bob@example.com=role1" into roles by lower-cased email.
func parseUserRoles(value string) map[string][]string {
	out := map[string][]string{}
	for _, entry := range strings.Split(value, ";") {
		email, list, ok := strings.Cut(entry, "=")
		email = strings.ToLower(strings.TrimSpace(email))
		if !ok || email == "" {
			continue
		}
		for _, role := range strings.Split(list, ",") {
			if role = strings.TrimSpace(role); role != "" {
				out[email] = append(out[email], role)
			}
		}
	}
	return out
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
