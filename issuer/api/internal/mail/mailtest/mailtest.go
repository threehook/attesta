// Package mailtest gives tests a real mail server (Mailpit) in a container.
package mailtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type Server struct {
	Host     string
	SMTPPort int
	api      string
}

var (
	once     sync.Once
	server   Server
	startErr error
)

// Start runs one Mailpit per test binary, which the container reaper removes afterwards, and skips the test when Docker is not available.
func Start(t *testing.T) Server {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	once.Do(func() {
		ctx := context.Background()
		c, err := testcontainers.Run(ctx, "axllent/mailpit:latest",
			testcontainers.WithExposedPorts("1025/tcp", "8025/tcp"),
			testcontainers.WithWaitStrategy(wait.ForHTTP("/livez").WithPort("8025/tcp")))
		if err != nil {
			startErr = err
			return
		}
		host, _ := c.Host(ctx)
		smtp, _ := c.MappedPort(ctx, "1025/tcp")
		api, _ := c.MappedPort(ctx, "8025/tcp")
		port, _ := strconv.Atoi(smtp.Port())
		server = Server{Host: host, SMTPPort: port, api: fmt.Sprintf("http://%s:%s", host, api.Port())}
	})
	if startErr != nil {
		t.Fatalf("start mailpit: %v", startErr)
	}
	return server
}

type Message struct {
	ID      string
	Subject string
	To      []string
	Raw     string
}

// To lists the messages sent to the address, newest first, with their raw source.
func (s Server) To(t *testing.T, address string) []Message {
	t.Helper()
	var list struct {
		Messages []struct {
			ID      string
			Subject string
			To      []struct{ Address string }
		}
	}
	s.getJSON(t, "/api/v1/search?query=to:"+address, &list)
	var out []Message
	for _, m := range list.Messages {
		raw, err := http.Get(s.api + "/api/v1/message/" + m.ID + "/raw")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(raw.Body)
		raw.Body.Close()
		msg := Message{ID: m.ID, Subject: m.Subject, Raw: string(body)}
		for _, to := range m.To {
			msg.To = append(msg.To, to.Address)
		}
		out = append(out, msg)
	}
	return out
}

func (s Server) getJSON(t *testing.T, path string, into any) {
	t.Helper()
	res, err := http.Get(s.api + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}
