package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckFindsNewerManifest(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+ManifestName {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{
			"schema": 1,
			"version": "0.9.0",
			"binary": "ltop",
			"assets": {
				"linux/amd64": {"name": "ltop_linux_amd64.tar.gz", "sha256": %q}
			}
		}`, hash)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.GOOS, c.GOARCH = "linux", "amd64"
	av, err := c.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if av == nil {
		t.Fatal("expected an update")
	}
	if av.Version != "0.9.0" || av.Asset.Name != "ltop_linux_amd64.tar.gz" {
		t.Errorf("available = %+v", av)
	}
}

func TestCheckSilentWhenCurrent(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"schema":1,"version":"0.1.0","binary":"ltop","assets":{"linux/amd64":{"name":"ltop_linux_amd64.tar.gz","sha256":%q}}}`, hash)
	}))
	defer srv.Close()

	c := testClient(srv)
	av, err := c.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if av != nil {
		t.Errorf("got update %+v, want none", av)
	}
}

func TestCheckSkipsDevBuilds(t *testing.T) {
	c := &Client{Base: "https://example.invalid", GOOS: "linux", GOARCH: "amd64"}
	av, err := c.Check(context.Background(), "dev")
	if err != nil || av != nil {
		t.Errorf("dev build: av=%v err=%v, want nil,nil", av, err)
	}
}

func TestCheckFallsBackToChecksums(t *testing.T) {
	payload := []byte("hello-archive")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + ManifestName:
			http.NotFound(w, r)
		case "/" + ChecksumsName:
			fmt.Fprintf(w, "%s  ltop_linux_amd64.tar.gz\n", hexSum)
		case "/latest":
			w.Header().Set("Location", srvURL(r, "/tag/v0.8.0"))
			w.WriteHeader(http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := testClient(srv)
	c.LatestURL = srv.URL + "/latest"
	av, err := c.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if av == nil || av.Version != "0.8.0" {
		t.Fatalf("fallback available = %+v err=%v", av, err)
	}
	if av.Asset.SHA256 != hexSum {
		t.Errorf("sha256 = %s", av.Asset.SHA256)
	}
}

func TestCheckNetworkErrorIsNotFatal(t *testing.T) {
	c := &Client{
		Base:   "https://127.0.0.1:1",
		HTTP:   &http.Client{},
		GOOS:   "linux",
		GOARCH: "amd64",
	}
	av, err := c.Check(context.Background(), "0.1.0")
	if av != nil {
		t.Errorf("got update on network error: %+v", av)
	}
	if err == nil {
		t.Fatal("expected an error to report (caller must ignore it)")
	}
}

func testClient(srv *httptest.Server) *Client {
	return &Client{
		Base:   srv.URL,
		HTTP:   srv.Client(),
		GOOS:   "linux",
		GOARCH: "amd64",
	}
}

func srvURL(r *http.Request, path string) string {
	return "https://" + r.Host + path
}
