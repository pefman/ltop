package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These names are the v1 self-update contract. Changing them strands every
// already-shipped binary that knows how to upgrade.
func TestGoreleaserKeepsFrozenAssetNames(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller path")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{
		`name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"`,
		`name_template: checksums.txt`,
		"linux",
		"amd64",
		"arm64",
		"github.com/pefman/ltop/internal/buildinfo.Version={{ .Version }}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf(".goreleaser.yaml missing frozen contract fragment %q", want)
		}
	}
}
