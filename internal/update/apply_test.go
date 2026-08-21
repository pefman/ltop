package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyReplacesBinaryAfterChecksum(t *testing.T) {
	newBin := elfish("new-ltop-binary-contents")
	archive := tarGz(t, map[string][]byte{BinaryName: newBin})
	sum := sha256.Sum256(archive)
	hexSum := hex.EncodeToString(sum[:])

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+ArchiveName("linux", "amd64") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, BinaryName)
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := testClient(srv)
	c.NoExec = true
	c.Executable = func() (string, error) { return exe, nil }

	err := c.Apply(context.Background(), &Available{
		Version: "0.9.0",
		Asset:   Asset{Name: ArchiveName("linux", "amd64"), SHA256: hexSum},
		Base:    srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBin) {
		t.Errorf("installed %q, want new binary", got)
	}
	bak, err := os.ReadFile(exe + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != "old-binary" {
		t.Errorf("backup = %q", bak)
	}
}

func TestApplyRejectsChecksumMismatch(t *testing.T) {
	archive := tarGz(t, map[string][]byte{BinaryName: []byte("tampered")})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, BinaryName)
	original := []byte("old-binary")
	if err := os.WriteFile(exe, original, 0o755); err != nil {
		t.Fatal(err)
	}

	c := testClient(srv)
	c.NoExec = true
	c.Executable = func() (string, error) { return exe, nil }

	err := c.Apply(context.Background(), &Available{
		Version: "0.9.0",
		Asset:   Asset{Name: ArchiveName("linux", "amd64"), SHA256: hex.EncodeToString(bytes.Repeat([]byte{0}, 32))},
		Base:    srv.URL,
	})
	if err == nil {
		t.Fatal("expected checksum error")
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("binary changed after failed apply: %q", got)
	}
}

func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0755, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestApplyReplacesRunningBinary(t *testing.T) {
	// Linux returns ETXTBSY if we open a running executable for write.
	// Self-update must rename the old inode aside instead.
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not on PATH")
	}
	body, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, BinaryName)
	if err := os.WriteFile(exe, body, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	time.Sleep(50 * time.Millisecond)

	newBin := elfish("replacement")
	archive := tarGz(t, map[string][]byte{BinaryName: newBin})
	sum := sha256.Sum256(archive)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.NoExec = true
	c.Executable = func() (string, error) { return exe, nil }
	if err := c.Apply(context.Background(), &Available{
		Version: "0.9.0",
		Asset:   Asset{Name: ArchiveName("linux", "amd64"), SHA256: hex.EncodeToString(sum[:])},
		Base:    srv.URL,
	}); err != nil {
		t.Fatalf("apply running binary: %v", err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBin) {
		t.Errorf("path still holds the old inode contents")
	}
}

func TestApplyRejectsMissingBinaryInArchive(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"README.md": []byte("no binary here")})
	sum := sha256.Sum256(archive)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), BinaryName)
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := testClient(srv)
	c.NoExec = true
	c.Executable = func() (string, error) { return exe, nil }
	err := c.Apply(context.Background(), &Available{
		Asset: Asset{Name: ArchiveName("linux", "amd64"), SHA256: hex.EncodeToString(sum[:])},
		Base:  srv.URL,
	})
	if err == nil {
		t.Fatal("expected error when archive has no ltop binary")
	}
}

func elfish(payload string) []byte {
	return append([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}, []byte(payload)...)
}
