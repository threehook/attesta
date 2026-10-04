// Command laadpalen-api is the backend of the laadpalen example: municipality employees submit requests for a laadpaal (EV charging point). The page
// calls this server and nothing else; this server asks attesta, running as a sidecar in the same pod, whether the employee may submit, and then
// carries the request out.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
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

	srv := &http.Server{
		Addr:              addr,
		Handler:           newServer(newAttestaClient(attestaURL), logger).routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("listening", "addr", addr, "attesta", attestaURL)
	return srv.ListenAndServe()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
