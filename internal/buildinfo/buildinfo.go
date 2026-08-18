// Package buildinfo carries version information stamped at link time.
package buildinfo

// Version is ltop's own release version, set by GoReleaser or install.sh.
// It is deliberately distinct from the llama.cpp build string reported by a
// monitored server.
var Version = "dev"
