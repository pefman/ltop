package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

	gotPath, err := c.Apply(context.Background(), &Available{
		Version: "0.9.0",
		Asset:   Asset{Name: ArchiveName("linux", "amd64"), SHA256: hexSum},
		Base:    srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != exe {
		t.Errorf("Apply path = %q, want the original path %q (not .bak)", gotPath, exe)
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

	_, err := c.Apply(context.Background(), &Available{
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
	gotPath, err := c.Apply(context.Background(), &Available{
		Version: "0.9.0",
		Asset:   Asset{Name: ArchiveName("linux", "amd64"), SHA256: hex.EncodeToString(sum[:])},
		Base:    srv.URL,
	})
	if err != nil {
		t.Fatalf("apply running binary: %v", err)
	}
	if gotPath != exe {
		t.Errorf("restart path = %q, want %q; os.Executable after rename is .bak and would loop", gotPath, exe)
	}
	link, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", cmd.Process.Pid))
	if err == nil && !strings.HasSuffix(link, ".bak") && link != exe+".bak" {
		t.Logf("/proc/%d/exe = %s (expected the old inode)", cmd.Process.Pid, link)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBin) {
		t.Errorf("path still holds the old inode contents")
	}
}

func TestApplyRestartRunsNewVersionNotBak(t *testing.T) {
	// The TUI used to syscall.Exec(os.Executable()) after Apply. After the
	// rename that is ltop.bak — the old binary — so u looked like it worked
	// and then the update banner came back. Restart must use Apply's path.
	root := repoRoot(t)
	dir := t.TempDir()
	oldBin := filepath.Join(dir, "old")
	newBin := filepath.Join(dir, "new")
	buildLtop(t, root, oldBin, "1.0.0")
	buildLtop(t, root, newBin, "2.0.0")

	newBytes, err := os.ReadFile(newBin)
	if err != nil {
		t.Fatal(err)
	}
	archive := tarGz(t, map[string][]byte{BinaryName: newBytes})
	sum := sha256.Sum256(archive)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	exe := filepath.Join(dir, BinaryName)
	oldBytes, err := os.ReadFile(oldBin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, oldBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ltopVersion(t, exe); got != "ltop 1.0.0" {
		t.Fatalf("setup version = %q", got)
	}

	c := testClient(srv)
	c.NoExec = true
	c.Executable = func() (string, error) { return exe, nil }
	restart, err := c.Apply(context.Background(), &Available{
		Version: "2.0.0",
		Asset:   Asset{Name: ArchiveName("linux", "amd64"), SHA256: hex.EncodeToString(sum[:])},
		Base:    srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if restart != exe {
		t.Fatalf("restart path = %q, want %q", restart, exe)
	}
	if got := ltopVersion(t, restart); got != "ltop 2.0.0" {
		t.Errorf("exec Apply path: got %q, want ltop 2.0.0", got)
	}
	if got := ltopVersion(t, exe+".bak"); got != "ltop 1.0.0" {
		t.Errorf("bak (what os.Executable returns) = %q, want ltop 1.0.0", got)
	}
}

func buildLtop(t *testing.T, root, out, version string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out,
		"-ldflags", "-X github.com/pefman/ltop/internal/buildinfo.Version="+version,
		".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if outb, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", version, err, outb)
	}
}

func ltopVersion(t *testing.T, bin string) string {
	t.Helper()
	out, err := exec.Command(bin, "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s -version: %v\n%s", bin, err, out)
	}
	return strings.TrimSpace(string(out))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
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
	_, err := c.Apply(context.Background(), &Available{
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
