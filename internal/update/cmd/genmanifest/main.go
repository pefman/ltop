// Command genmanifest writes the frozen v1 update.json from a GoReleaser dist dir.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pefman/ltop/internal/update"
)

func main() {
	dist := flag.String("dist", "dist", "GoReleaser dist directory")
	version := flag.String("version", "", "release version, with or without v")
	out := flag.String("out", "", "output path (default: <dist>/update.json)")
	flag.Parse()

	if strings.TrimSpace(*version) == "" {
		fmt.Fprintln(os.Stderr, "genmanifest: -version is required")
		os.Exit(2)
	}
	if *out == "" {
		*out = filepath.Join(*dist, update.ManifestName)
	}
	sums, err := os.ReadFile(filepath.Join(*dist, update.ChecksumsName))
	if err != nil {
		fmt.Fprintf(os.Stderr, "genmanifest: %v\n", err)
		os.Exit(1)
	}
	raw, err := update.WriteManifest(*version, sums)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genmanifest: %v\n", err)
		os.Exit(1)
	}
	m, err := update.ParseManifest(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genmanifest: produced invalid update.json: %v\n", err)
		os.Exit(1)
	}
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		if _, ok := m.Assets[platform]; !ok {
			fmt.Fprintf(os.Stderr, "genmanifest: missing frozen asset %s\n", platform)
			os.Exit(1)
		}
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "genmanifest: %v\n", err)
		os.Exit(1)
	}
}
