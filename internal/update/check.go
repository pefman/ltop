package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxManifest  = 1 << 20
	maxChecksums = 1 << 20
	checkTimeout = 8 * time.Second
)

// Client talks to the frozen GitHub latest-download URLs.
type Client struct {
	Base      string
	LatestURL string
	HTTP      *http.Client
	GOOS      string
	GOARCH    string
	UserAgent string

	Executable func() (string, error)
	NoExec     bool
}

func (c *Client) base() string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return DefaultBase
}

func (c *Client) latestURL() string {
	if c.LatestURL != "" {
		return c.LatestURL
	}
	return DefaultLatest
}

func (c *Client) httpc() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP
}

var defaultHTTP = &http.Client{
	Timeout: 90 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-https redirect to %s", req.URL)
		}
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// Check looks up the latest release. A nil Available means "nothing to do"
// (already current, dev build, or this platform is missing). Network and
// parse failures return an error; the caller must not treat them as fatal.
func (c *Client) Check(ctx context.Context, current string) (*Available, error) {
	if !IsReleaseVersion(current) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	m, err := c.loadManifest(ctx)
	if err != nil {
		return nil, err
	}
	if !Newer(m.Version, current) {
		return nil, nil
	}
	asset, ok := m.Assets[Platform(c.GOOS, c.GOARCH)]
	if !ok {
		return nil, nil
	}
	return &Available{Version: m.Version, Asset: asset, Base: c.base()}, nil
}

// Check is the default-client entry used by the TUI.
func Check(ctx context.Context, current, goos, goarch string) (*Available, error) {
	c := &Client{GOOS: goos, GOARCH: goarch, UserAgent: "ltop/" + current}
	return c.Check(ctx, current)
}

func (c *Client) loadManifest(ctx context.Context) (Manifest, error) {
	body, status, err := c.get(ctx, c.base()+"/"+ManifestName, maxManifest)
	if err == nil && status == http.StatusOK {
		m, perr := ParseManifest(body)
		if perr == nil {
			return m, nil
		}
		err = perr
	}
	// Fallback: checksums.txt plus the /releases/latest redirect. Old
	// clients keep working even if update.json is missing from a release.
	fb, ferr := c.manifestFromChecksums(ctx)
	if ferr == nil {
		return fb, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{}, ferr
}

func (c *Client) manifestFromChecksums(ctx context.Context) (Manifest, error) {
	sums, status, err := c.get(ctx, c.base()+"/"+ChecksumsName, maxChecksums)
	if err != nil {
		return Manifest{}, err
	}
	if status != http.StatusOK {
		return Manifest{}, fmt.Errorf("checksums.txt: HTTP %d", status)
	}
	version, err := c.latestTag(ctx)
	if err != nil {
		return Manifest{}, err
	}
	raw, err := WriteManifest(version, sums)
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(raw)
}

func (c *Client) latestTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.latestURL(), nil)
	if err != nil {
		return "", err
	}
	c.setUA(req)
	cl := *c.httpc()
	cl.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if loc := resp.Header.Get("Location"); loc != "" {
		if v, ok := tagFromPath(loc); ok {
			return v, nil
		}
	}
	if v, ok := tagFromPath(resp.Request.URL.Path); ok {
		return v, nil
	}
	return "", fmt.Errorf("cannot read latest tag")
}

func tagFromPath(p string) (string, bool) {
	p = strings.TrimRight(p, "/")
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "", false
	}
	return CanonicalVersion(p[i+1:])
}

func (c *Client) get(ctx context.Context, url string, max int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	c.setUA(req)
	resp, err := c.httpc().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(body)) > max {
		return nil, resp.StatusCode, fmt.Errorf("response too large")
	}
	return body, resp.StatusCode, nil
}

func (c *Client) setUA(req *http.Request) {
	ua := c.UserAgent
	if ua == "" {
		ua = "ltop"
	}
	req.Header.Set("User-Agent", ua)
}
