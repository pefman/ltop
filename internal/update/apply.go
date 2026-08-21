package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	maxArchive   = 80 << 20
	applyTimeout = 60 * time.Second
)

var errChecksum = errors.New("sha256 mismatch")

// Apply downloads, verifies, and replaces the running binary. It returns the
// path that now holds the new file. Callers must exec that path — not
// os.Executable(), which after the rename points at <name>.bak (the old
// inode) and would restart the previous version in a loop.
//
// On success it execs the new file unless NoExec is set (tests). The previous
// binary is kept next to it as <name>.bak so a failed exec can be rolled back.
func (c *Client) Apply(ctx context.Context, av *Available) (string, error) {
	if av == nil || av.Asset.Name == "" || av.Asset.SHA256 == "" {
		return "", fmt.Errorf("no update to apply")
	}
	ctx, cancel := context.WithTimeout(ctx, applyTimeout)
	defer cancel()

	exe, err := c.exe()
	if err != nil {
		return "", err
	}
	if err := writableDir(filepath.Dir(exe)); err != nil {
		return "", err
	}

	url := strings.TrimRight(av.Base, "/") + "/" + av.Asset.Name
	body, status, err := c.get(ctx, url, maxArchive)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("download: HTTP %d", status)
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != strings.ToLower(av.Asset.SHA256) {
		return "", errChecksum
	}
	bin, err := extractBinary(body)
	if err != nil {
		return "", err
	}

	bak := exe + ".bak"
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(exe, bak); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("backup current binary: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(bak, exe)
		os.Remove(tmp)
		return "", fmt.Errorf("install new binary: %w", err)
	}

	if c.NoExec {
		return exe, nil
	}
	err = syscall.Exec(exe, os.Args, os.Environ())
	_ = os.Rename(bak, exe)
	return "", err
}

func (c *Client) exe() (string, error) {
	fn := c.Executable
	if fn == nil {
		fn = os.Executable
	}
	path, err := fn()
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	return path, nil
}

// writableDir checks that we can create a sibling file. Opening the running
// binary itself with O_WRONLY fails on Linux with ETXTBSY; rename-aside
// of that inode is what actually replaces it.
func writableDir(dir string) error {
	f, err := os.CreateTemp(dir, ".ltop-write-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

func extractBinary(archive []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("archive: %w", err)
		}
		name := strings.ReplaceAll(hdr.Name, "\\", "/")
		if strings.Contains(name, "..") {
			continue
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(name) != BinaryName {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxArchive+1))
		if err != nil {
			return nil, err
		}
		if len(data) == 0 || int64(len(data)) > maxArchive {
			return nil, fmt.Errorf("archive: binary has an implausible size")
		}
		if !bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
			return nil, fmt.Errorf("archive: ltop is not an ELF binary")
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive: no %s binary", BinaryName)
}
