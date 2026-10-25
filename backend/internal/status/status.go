// Package status finds out whether a credential was revoked: it reads the issuer's signed status list (draft-ietf-oauth-status-list) and looks at
// the credential's bit. It fails closed: a credential without a status reference, a list that cannot be read or has expired, and a list that does
// not match the credential are all refused.
package status

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/threehook/sdjwt-go"
)

var (
	// ErrRevoked is a credential whose bit in the issuer's list is set.
	ErrRevoked = errors.New("credential has been revoked")
	// ErrNoStatus is a credential that cannot be checked because it has no status reference.
	ErrNoStatus = errors.New("credential has no status reference")
	// ErrUnavailable is every reason the status could not be established.
	ErrUnavailable = errors.New("credential status could not be established")
)

const (
	defaultTTL = time.Minute
	// refetchGap is the least time between two fetches of one list for a credential that lies beyond the list we have.
	refetchGap   = 5 * time.Second
	maxListBytes = 4 << 20
)

type Options struct {
	// Prefixes are the URLs status lists may be fetched from, each ending in "/". A list anywhere else is refused.
	Prefixes []string
	// Hosts sends a URL that starts with a key to the base URL in its value, for a list that is reached another way than its public address.
	Hosts map[string]string
	Keys  sdjwt.KeyResolver
	// Client defaults to a client with a 5 second timeout.
	Client *http.Client
	// Now defaults to time.Now.
	Now func() time.Time
}

type Checker struct {
	prefixes []string
	hosts    map[string]string
	keys     sdjwt.KeyResolver
	client   *http.Client
	now      func() time.Time

	mu    sync.Mutex
	lists map[string]cached
}

type cached struct {
	token     *sdjwt.StatusListToken
	fetchedAt time.Time
	// validUntil is when the list must be fetched again: its ttl, or its expiry if that is sooner.
	validUntil time.Time
}

// New returns a checker; it refuses a prefix that does not end in "/", which would also match other hosts.
func New(o Options) (*Checker, error) {
	for _, p := range o.Prefixes {
		if u, err := url.Parse(p); err != nil || u.Host == "" || !strings.HasSuffix(p, "/") {
			return nil, fmt.Errorf("status list prefix %q must be an URL that ends in /", p)
		}
	}

	for from := range o.Hosts {
		if !strings.HasSuffix(from, "/") && strings.Contains(strings.TrimPrefix(strings.TrimPrefix(from, "https://"), "http://"), "/") {
			return nil, fmt.Errorf("status host %q must be a scheme and host", from)
		}
	}

	c := &Checker{prefixes: o.Prefixes, hosts: o.Hosts, keys: o.Keys, client: o.Client, now: o.Now, lists: map[string]cached{}}
	if c.client == nil {
		c.client = &http.Client{Timeout: 5 * time.Second}
	}

	if c.now == nil {
		c.now = time.Now
	}

	return c, nil
}

// Check is nil when the credential issued by issuer, with the status reference ref, is not revoked.
func (c *Checker) Check(ctx context.Context, issuer string, ref *sdjwt.StatusReference) error {
	if ref == nil {
		return ErrNoStatus
	}

	if !c.allowed(ref.URI) {
		return fmt.Errorf("%w: %s is not an allowed status list", ErrUnavailable, ref.URI)
	}

	token, err := c.list(ctx, ref)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	if token.Issuer != issuer {
		return fmt.Errorf("%w: the list is signed by %s, not by the issuer of the credential", ErrUnavailable, token.Issuer)
	}

	revoked, err := token.List.Revoked(ref.Index)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	if revoked {
		return ErrRevoked
	}

	return nil
}

func (c *Checker) allowed(uri string) bool {
	for _, p := range c.prefixes {
		if strings.HasPrefix(uri, p) {
			return true
		}
	}

	return false
}

// list gives the list at ref.URI: the one we have if it is still good and holds the credential's position, else a fresh one.
func (c *Checker) list(ctx context.Context, ref *sdjwt.StatusReference) (*sdjwt.StatusListToken, error) {
	now := c.now()
	c.mu.Lock()
	have, ok := c.lists[ref.URI]
	c.mu.Unlock()

	if ok && now.Before(have.validUntil) && (ref.Index < have.token.List.Len() || now.Sub(have.fetchedAt) < refetchGap) {
		return have.token, nil
	}

	token, err := c.fetch(ctx, ref.URI)
	if err != nil {
		return nil, err
	}

	ttl := token.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}

	valid := now.Add(ttl)
	if !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(valid) {
		valid = token.ExpiresAt
	}

	c.mu.Lock()
	c.lists[ref.URI] = cached{token: token, fetchedAt: now, validUntil: valid}
	c.mu.Unlock()

	return token, nil
}

func (c *Checker) fetch(ctx context.Context, uri string) (*sdjwt.StatusListToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.reach(uri), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/statuslist+jwt")

	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching the status list: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the status list answered %d", res.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxListBytes+1))
	if err != nil || len(body) > maxListBytes {
		return nil, errors.New("the status list could not be read or is too large")
	}

	token, err := sdjwt.VerifyStatusList(strings.TrimSpace(string(body)), c.keys, c.now)
	if err != nil {
		return nil, err
	}

	if token.URI != uri {
		return nil, fmt.Errorf("the list says it is %s", token.URI)
	}

	return token, nil
}

// reach is the address to fetch uri from.
func (c *Checker) reach(uri string) string {
	for from, to := range c.hosts {
		if strings.HasPrefix(uri, from) {
			return strings.TrimSuffix(to, "/") + strings.TrimPrefix(uri, strings.TrimSuffix(from, "/"))
		}
	}

	return uri
}
