// Package mail sends the issuer's mails.
package mail

import (
	"bytes"
	"context"
	"fmt"

	gomail "github.com/wneessen/go-mail"

	"attesta/issuer/internal/config"
)

type Message struct {
	To      string
	Subject string
	Text    string
	// HTML may show Image with <img src="cid:..."> of its ContentID.
	HTML  string
	Image *Image
}

type Image struct {
	Filename  string
	ContentID string
	PNG       []byte
}

type Mailer interface {
	Send(ctx context.Context, m Message) error
}

type SMTP struct{ cfg config.SMTP }

func NewSMTP(cfg config.SMTP) *SMTP { return &SMTP{cfg: cfg} }

func (s *SMTP) options() ([]gomail.Option, error) {
	opts := []gomail.Option{gomail.WithPort(s.cfg.Port)}
	switch s.cfg.TLS {
	case "none":
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	case "starttls":
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	case "tls":
		opts = append(opts, gomail.WithSSL())
	case "":
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSOpportunistic))
	default:
		return nil, fmt.Errorf("unknown TLS mode %q", s.cfg.TLS)
	}
	if s.cfg.User != "" {
		opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover), gomail.WithUsername(s.cfg.User), gomail.WithPassword(s.cfg.Password))
	}
	return opts, nil
}

func (s *SMTP) Send(ctx context.Context, m Message) error {
	opts, err := s.options()
	if err != nil {
		return err
	}
	client, err := gomail.NewClient(s.cfg.Host, opts...)
	if err != nil {
		return err
	}
	msg := gomail.NewMsg()
	if err := msg.From(s.cfg.From); err != nil {
		return err
	}
	if err := msg.To(m.To); err != nil {
		return err
	}
	msg.Subject(m.Subject)
	msg.SetBodyString(gomail.TypeTextPlain, m.Text)
	msg.AddAlternativeString(gomail.TypeTextHTML, m.HTML)
	if m.Image != nil {
		err := msg.EmbedReader(m.Image.Filename, bytes.NewReader(m.Image.PNG),
			gomail.WithFileContentID("<"+m.Image.ContentID+">"), gomail.WithFileContentType(gomail.ContentType("image/png")))
		if err != nil {
			return err
		}
	}
	return client.DialAndSendWithContext(ctx, msg)
}
