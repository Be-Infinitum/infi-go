package infi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// URLs derived from the key prefix.
const (
	SandboxURL = "https://api-sandbox.beinfi.com"
	LiveURL    = "https://api.beinfi.com"
)

// Channel is the origin channel of an approval (X-Infi-Channel header).
type Channel string

const (
	ChannelPanel Channel = "panel"
	ChannelMCP   Channel = "mcp"
	ChannelCLI   Channel = "cli"
	ChannelAPI   Channel = "api"
)

// Client talks to the public API. It is safe for concurrent use.
// The services (client.Checkouts, client.Products...) come from resources.gen.go.
type Client struct {
	Resources

	secretKey   string
	baseURL     string
	http        *http.Client
	maxAttempts int
	sleep       func(ctx context.Context, d time.Duration) error
}

type clientConfig struct {
	apiURL      string
	http        *http.Client
	maxAttempts int
}

// Option configures the Client.
type Option func(*clientConfig)

// WithAPIURL overrides the URL derived from the key prefix.
func WithAPIURL(url string) Option { return func(c *clientConfig) { c.apiURL = url } }

// WithHTTPClient replaces the *http.Client (proxy, timeouts, tests).
func WithHTTPClient(h *http.Client) Option { return func(c *clientConfig) { c.http = h } }

// WithMaxAttempts changes the total number of attempts (default 3).
func WithMaxAttempts(n int) Option { return func(c *clientConfig) { c.maxAttempts = n } }

// BaseURLFor returns the base URL for the key, or apiURL when given.
func BaseURLFor(secretKey, apiURL string) (string, error) {
	if apiURL != "" {
		return strings.TrimRight(apiURL, "/"), nil
	}
	switch {
	case strings.HasPrefix(secretKey, "sk_test_"):
		return SandboxURL, nil
	case strings.HasPrefix(secretKey, "sk_live_"):
		return LiveURL, nil
	}
	return "", errors.New("Infi: the key must start with sk_test_ or sk_live_ (or use WithAPIURL)")
}

// New creates the client. sk_test_ points to the sandbox, sk_live_ to production.
func New(secretKey string, opts ...Option) (*Client, error) {
	if secretKey == "" {
		return nil, errors.New("Infi: the secret key is required")
	}
	cfg := clientConfig{maxAttempts: 3}
	for _, o := range opts {
		o(&cfg)
	}
	base, err := BaseURLFor(secretKey, cfg.apiURL)
	if err != nil {
		return nil, err
	}
	if cfg.http == nil {
		cfg.http = http.DefaultClient
	}
	if cfg.maxAttempts < 1 {
		cfg.maxAttempts = 1
	}
	c := &Client{secretKey: secretKey, baseURL: base, http: cfg.http, maxAttempts: cfg.maxAttempts, sleep: sleepCtx}
	c.Resources = newResources(c)
	return c, nil
}

// CallOption adjusts a call.
type CallOption func(*callConfig)

type callConfig struct {
	idempotencyKey string
	ifMatch        *string
	channel        Channel
}

// WithIdempotencyKey sets the Idempotency-Key (default: a UUID v4 generated on mutations).
func WithIdempotencyKey(key string) CallOption {
	return func(c *callConfig) { c.idempotencyKey = key }
}

// WithIfMatch sends If-Match (hash of the preview or the plan).
func WithIfMatch(v string) CallOption { return func(c *callConfig) { c.ifMatch = &v } }

// WithChannel sends X-Infi-Channel.
func WithChannel(ch Channel) CallOption { return func(c *callConfig) { c.channel = ch } }

// Ptr returns a pointer to v, for optional fields of the generated types.
func Ptr[T any](v T) *T { return &v }
