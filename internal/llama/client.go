// Package llama is a read-only client for a llama.cpp server's native
// telemetry endpoints.
package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pefman/ltop/internal/promparse"
)

// maxBody caps a response read so a misidentified endpoint cannot exhaust memory.
const maxBody = 8 << 20

// Client talks to one llama.cpp server.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for base, which may include or omit a /v1 suffix.
func New(base string) *Client {
	return &Client{
		base: NormalizeBase(base),
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}
}

// BaseURL returns the server root the client targets.
func (c *Client) BaseURL() string { return c.base }

// NormalizeBase trims a trailing /v1 and any trailing slash, so callers may
// paste either the OpenAI-compatible URL or the server root.
func NormalizeBase(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, "/v1")
	return strings.TrimRight(s, "/")
}

// ValidBase reports whether raw parses as an http(s) URL with a host.
func ValidBase(raw string) bool {
	u, err := url.Parse(NormalizeBase(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", path, resp.Status)
	}
	return body, nil
}

func getJSON[T any](ctx context.Context, c *Client, path string) (T, error) {
	var out T
	body, err := c.get(ctx, path)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// Health queries /health.
func (c *Client) Health(ctx context.Context) (Health, error) {
	return getJSON[Health](ctx, c, "/health")
}

// Props queries /props for model and server configuration.
func (c *Client) Props(ctx context.Context) (Props, error) {
	return getJSON[Props](ctx, c, "/props")
}

// Slots queries /slots. It returns ErrSlotsDisabled when the server was
// started without slot introspection.
func (c *Client) Slots(ctx context.Context) ([]Slot, error) {
	body, err := c.get(ctx, "/slots")
	if err != nil {
		if strings.Contains(err.Error(), "501") {
			return nil, ErrSlotsDisabled
		}
		return nil, err
	}
	var slots []Slot
	if err := json.Unmarshal(body, &slots); err != nil {
		return nil, fmt.Errorf("/slots: %w", err)
	}
	return slots, nil
}

// Metrics queries /metrics and parses the Prometheus exposition document.
func (c *Client) Metrics(ctx context.Context) (promparse.Set, error) {
	body, err := c.get(ctx, "/metrics")
	if err != nil {
		return nil, err
	}
	return promparse.Parse(strings.NewReader(string(body)))
}

// Models queries the OpenAI-compatible /v1/models endpoint.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	body, err := c.get(ctx, "/v1/models")
	if err != nil {
		return nil, err
	}
	return decodeModels(body)
}

// ErrSlotsDisabled reports that the server refused slot introspection.
var ErrSlotsDisabled = fmt.Errorf("slots endpoint disabled on server")
