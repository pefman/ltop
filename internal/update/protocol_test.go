package update

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArchiveNameIsFrozen(t *testing.T) {
	if got := ArchiveName("linux", "amd64"); got != "ltop_linux_amd64.tar.gz" {
		t.Errorf("amd64 archive = %q", got)
	}
	if got := ArchiveName("linux", "arm64"); got != "ltop_linux_arm64.tar.gz" {
		t.Errorf("arm64 archive = %q", got)
	}
}

func TestParseManifestIgnoresUnknownFields(t *testing.T) {
	raw := `{
		"schema": 1,
		"version": "0.3.0",
		"binary": "ltop",
		"future_flag": true,
		"assets": {
			"linux/amd64": {
				"name": "ltop_linux_amd64.tar.gz",
				"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				"extra": "ignored"
			}
		}
	}`
	m, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0.3.0" || m.Binary != "ltop" {
		t.Errorf("manifest = %+v", m)
	}
	a, ok := m.Assets["linux/amd64"]
	if !ok || a.Name != "ltop_linux_amd64.tar.gz" {
		t.Errorf("asset = %+v", a)
	}
}

func TestParseManifestSchema2StillReadable(t *testing.T) {
	// A future schema must keep the v1 keys so old binaries can still upgrade.
	raw := `{
		"schema": 2,
		"version": "1.0.0",
		"binary": "ltop",
		"assets": {
			"linux/arm64": {
				"name": "ltop_linux_arm64.tar.gz",
				"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
		}
	}`
	m, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("schema 2 must remain readable by v1 clients: %v", err)
	}
	if m.Version != "1.0.0" {
		t.Errorf("version = %q", m.Version)
	}
}

func TestParseManifestRejectsBadAssetName(t *testing.T) {
	raw := `{
		"schema": 1,
		"version": "0.3.0",
		"assets": {
			"linux/amd64": {
				"name": "ltop-linux-amd64.zip",
				"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
			}
		}
	}`
	if _, err := ParseManifest([]byte(raw)); err == nil {
		t.Fatal("expected error for non-contract asset name")
	}
}

func TestWriteAndParseManifestRoundTrip(t *testing.T) {
	sums := strings.Join([]string{
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  ltop_linux_amd64.tar.gz",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  ltop_linux_arm64.tar.gz",
	}, "\n") + "\n"

	raw, err := WriteManifest("0.4.0", []byte(sums))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if probe["schema"].(float64) != 1 {
		t.Errorf("schema = %v, want 1", probe["schema"])
	}

	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0.4.0" || m.Binary != BinaryName {
		t.Errorf("manifest = %+v", m)
	}
	if m.Assets["linux/amd64"].SHA256 != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Errorf("amd64 hash = %s", m.Assets["linux/amd64"].SHA256)
	}
	if m.Assets["linux/arm64"].Name != "ltop_linux_arm64.tar.gz" {
		t.Errorf("arm64 name = %s", m.Assets["linux/arm64"].Name)
	}
}

func TestNewerVersions(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.10.0", "0.9.0", true},
		{"v0.10.0", "0.9.9", true},
		{"1.0.0", "0.9.9", true},
		{"0.1.1", "0.1.0", true},
	}
	for _, tc := range cases {
		if got := Newer(tc.latest, tc.current); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestIsReleaseVersion(t *testing.T) {
	if !IsReleaseVersion("0.1.0") || !IsReleaseVersion("v1.2.3") {
		t.Error("clean tags should count as releases")
	}
	for _, v := range []string{"dev", "", "v0.1.0-5-gabcdef", "0.1.0-dirty", "0.1.0-next"} {
		if IsReleaseVersion(v) {
			t.Errorf("%q counted as a release", v)
		}
	}
}
