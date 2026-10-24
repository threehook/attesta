package mail_test

import (
	"attesta/issuer/internal/config"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/mail/mailtest"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSendsTheMailWithTheImageInline(t *testing.T) {
	server := mailtest.Start(t)
	to := uuid.NewString() + "@example.com"
	smtp := mail.NewSMTP(config.SMTP{Host: server.Host, Port: server.SMTPPort, From: "credentials@attesta.local"})

	err := smtp.Send(context.Background(), mail.Message{
		To: to, Subject: "Uw credential staat klaar bij Gemeente Zoëterwoude", Text: "platte tekst", HTML: `<p>hallo</p><img src="cid:qr">`,
		Image: &mail.Image{Filename: "credential.png", ContentID: "qr", PNG: []byte("\x89PNG-bytes")},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := server.To(t, to)
	if len(got) != 1 || got[0].Subject != "Uw credential staat klaar bij Gemeente Zoëterwoude" {
		t.Fatalf("messages %+v", got)
	}

	raw := strings.ToLower(got[0].Raw)
	for _, want := range []string{
		"multipart/related", "text/plain", "text/html", "content-id: <qr>", "filename=\"credential.png\"",
		"from: <credentials@attesta.local>",
	} {
		if !strings.Contains(raw, strings.ToLower(want)) {
			t.Errorf("the mail lacks %q:\n%s", want, got[0].Raw)
		}
	}

	if strings.Count(raw, "filename=\"credential.png\"") != 1 {
		t.Errorf("the image is in the mail more than once")
	}
}

func TestRefusesAnUnknownTLSMode(t *testing.T) {
	smtp := mail.NewSMTP(config.SMTP{Host: "localhost", Port: 1025, TLS: "ssl", From: "a@b.nl"})
	if err := smtp.Send(context.Background(), mail.Message{To: "x@example.com"}); err == nil {
		t.Fatal("no error")
	}
}

func TestReportsAServerThatCannotBeReached(t *testing.T) {
	smtp := mail.NewSMTP(config.SMTP{Host: "127.0.0.1", Port: 1, TLS: "none", From: "a@b.nl"})
	if err := smtp.Send(context.Background(), mail.Message{To: "x@example.com", Text: "t", HTML: "h"}); err == nil {
		t.Fatal("no error")
	}
}
