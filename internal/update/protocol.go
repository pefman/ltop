// Package update implements ltop's self-update protocol v1.
//
// The contract is frozen so already-shipped binaries can always find a
// newer release. Do not rename these files, change the archive layout, or
// remove v1 fields from update.json:
//
//	https://github.com/pefman/ltop/releases/latest/download/update.json
//	https://github.com/pefman/ltop/releases/latest/download/checksums.txt
//	https://github.com/pefman/ltop/releases/latest/download/ltop_linux_amd64.tar.gz
//	https://github.com/pefman/ltop/releases/latest/download/ltop_linux_arm64.tar.gz
//
// update.json schema 1:
//
//	{
//	  "schema": 1,
//	  "version": "0.2.0",
//	  "binary": "ltop",
//	  "assets": {
//	    "linux/amd64": {"name": "ltop_linux_amd64.tar.gz", "sha256": "<64 hex>"},
//	    "linux/arm64": {"name": "ltop_linux_arm64.tar.gz", "sha256": "<64 hex>"}
//	  }
//	}
//
// Unknown JSON fields are ignored. A higher schema is still accepted if the
// v1 keys remain. Archives are gzipped tarballs with the executable named
// "ltop" at the top level. SHA-256 is hex, lowercase, of the archive bytes.
package update

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	Schema        = 1
	BinaryName    = "ltop"
	ManifestName  = "update.json"
	ChecksumsName = "checksums.txt"
	DefaultOwner  = "pefman"
	DefaultRepo   = "ltop"
	DefaultBase   = "https://github.com/pefman/ltop/releases/latest/download"
	DefaultLatest = "https://github.com/pefman/ltop/releases/latest"
)

// Manifest is the v1 update document published with every GitHub release.
type Manifest struct {
	Schema  int              `json:"schema"`
	Version string           `json:"version"`
	Binary  string           `json:"binary"`
	Assets  map[string]Asset `json:"assets"`
}

// Asset is one OS/arch archive in a Manifest.
type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Available is a newer release the current binary can install.
type Available struct {
	Version string
	Asset   Asset
	Base    string
}

// ArchiveName is the frozen GoReleaser archive name for GOOS/GOARCH.
func ArchiveName(goos, goarch string) string {
	return BinaryName + "_" + goos + "_" + goarch + ".tar.gz"
}

// Platform is the assets-map key for GOOS/GOARCH, e.g. "linux/amd64".
func Platform(goos, goarch string) string { return goos + "/" + goarch }

// ManualInstall is the one-liner shown when self-update cannot replace the binary.
func ManualInstall(goos, goarch string) string {
	return "curl -sSL " + DefaultBase + "/" + ArchiveName(goos, goarch) +
		" | tar xz && install -Dm755 ltop ~/.local/bin/ltop"
}

// ParseManifest reads a v1 (or forward-compatible) update.json.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("update.json: %w", err)
	}
	if m.Version == "" {
		return Manifest{}, fmt.Errorf("update.json: missing version")
	}
	if _, ok := CanonicalVersion(m.Version); !ok {
		return Manifest{}, fmt.Errorf("update.json: invalid version %q", m.Version)
	}
	if m.Binary == "" {
		m.Binary = BinaryName
	}
	if m.Binary != BinaryName {
		return Manifest{}, fmt.Errorf("update.json: binary %q, want %q", m.Binary, BinaryName)
	}
	if len(m.Assets) == 0 {
		return Manifest{}, fmt.Errorf("update.json: no assets")
	}
	for platform, a := range m.Assets {
		osName, arch, ok := strings.Cut(platform, "/")
		if !ok || ArchiveName(osName, arch) != a.Name {
			return Manifest{}, fmt.Errorf("update.json: asset %q name %q is not the frozen archive name", platform, a.Name)
		}
		sum := strings.ToLower(strings.TrimSpace(a.SHA256))
		if _, err := hex.DecodeString(sum); err != nil || len(sum) != 64 {
			return Manifest{}, fmt.Errorf("update.json: asset %q has a bad sha256", platform)
		}
		a.SHA256 = sum
		m.Assets[platform] = a
	}
	return m, nil
}

// ParseChecksums reads a sha256sum file into archive-name → hex digest.
func ParseChecksums(data []byte) (map[string]string, error) {
	out := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := strings.ToLower(strings.TrimLeft(fields[0], "\\"))
		name := strings.TrimPrefix(fields[1], "*") // binary-mode marker
		if _, err := hex.DecodeString(sum); err != nil || len(sum) != 64 {
			continue
		}
		out[name] = sum
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("checksums.txt: no hashes")
	}
	return out, nil
}

// WriteManifest builds a v1 update.json from a checksums.txt body.
func WriteManifest(version string, checksums []byte) ([]byte, error) {
	canon, ok := CanonicalVersion(version)
	if !ok {
		return nil, fmt.Errorf("invalid version %q", version)
	}
	sums, err := ParseChecksums(checksums)
	if err != nil {
		return nil, err
	}
	m := Manifest{
		Schema:  Schema,
		Version: canon,
		Binary:  BinaryName,
		Assets:  make(map[string]Asset),
	}
	for name, sum := range sums {
		platform, ok := platformFromArchive(name)
		if !ok {
			continue
		}
		m.Assets[platform] = Asset{Name: name, SHA256: sum}
	}
	if len(m.Assets) == 0 {
		return nil, fmt.Errorf("checksums.txt: no ltop archives")
	}
	return json.MarshalIndent(m, "", "  ")
}

func platformFromArchive(name string) (string, bool) {
	if !strings.HasPrefix(name, BinaryName+"_") || !strings.HasSuffix(name, ".tar.gz") {
		return "", false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(name, BinaryName+"_"), ".tar.gz")
	goos, goarch, ok := strings.Cut(mid, "_")
	if !ok || goos == "" || goarch == "" || strings.Contains(goarch, "_") {
		return "", false
	}
	if ArchiveName(goos, goarch) != name {
		return "", false
	}
	return Platform(goos, goarch), true
}

// CanonicalVersion returns "X.Y.Z" for a clean release tag.
func CanonicalVersion(v string) (string, bool) {
	maj, min, pat, ok := splitVersion(v)
	if !ok {
		return "", false
	}
	return strconv.Itoa(maj) + "." + strconv.Itoa(min) + "." + strconv.Itoa(pat), true
}

// IsReleaseVersion reports a stamped X.Y.Z with no extra suffix.
func IsReleaseVersion(v string) bool {
	_, ok := CanonicalVersion(v)
	return ok
}

// Newer reports whether latest is a higher X.Y.Z than current.
func Newer(latest, current string) bool {
	lm, ln, lp, ok := splitVersion(latest)
	if !ok {
		return false
	}
	cm, cn, cp, ok := splitVersion(current)
	if !ok {
		return false
	}
	if lm != cm {
		return lm > cm
	}
	if ln != cn {
		return ln > cn
	}
	return lp > cp
}

func splitVersion(v string) (maj, min, pat int, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	for _, p := range parts {
		if p == "" || !allDigits(p) {
			return 0, 0, 0, false
		}
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	pat, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return maj, min, pat, true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
