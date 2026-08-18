#!/usr/bin/env bash
# Builds ltop from source and installs it into ~/.local/bin.
set -euo pipefail

BINARY=ltop
INSTALL_DIR="$HOME/.local/bin"
REQUIRED_GO_MINOR=22

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SRC_DIR"

info() { printf '\033[1;34m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$1" >&2; }
die() {
	printf '\033[1;31merror:\033[0m %s\n' "$1" >&2
	exit 1
}

if [[ "$(uname -s)" != "Linux" ]]; then
	die "ltop is Linux only (detected $(uname -s))"
fi

command -v go >/dev/null 2>&1 ||
	die "Go is not installed. See https://go.dev/dl/"

# go version prints e.g. "go version go1.26.1 linux/amd64".
go_version="$(go version | awk '{print $3}' | sed 's/^go//')"
go_major="${go_version%%.*}"
go_minor="$(printf '%s\n' "$go_version" | cut -d. -f2)"
if [[ "$go_major" -lt 1 ]] || { [[ "$go_major" -eq 1 ]] && [[ "$go_minor" -lt "$REQUIRED_GO_MINOR" ]]; }; then
	die "Go 1.$REQUIRED_GO_MINOR or newer is required (found $go_version)"
fi
info "Go $go_version"

info "Running tests"
go test ./... >/dev/null || die "tests failed; not installing"

# Prefer a tag, fall back to the commit, so -version means something.
if version="$(git -C "$SRC_DIR" describe --tags --always --dirty 2>/dev/null)"; then
	:
else
	version="dev"
fi

info "Building $BINARY $version"
go build -trimpath -ldflags "-s -w -X github.com/pefman/ltop/internal/buildinfo.Version=$version" -o "$BINARY" .

info "Installing to $INSTALL_DIR/$BINARY"
install -Dm755 "$BINARY" "$INSTALL_DIR/$BINARY"
rm -f "$BINARY"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	warn "$INSTALL_DIR is not on your PATH. Add this to your shell profile:"
	printf '\n    export PATH="%s:$PATH"\n\n' "$INSTALL_DIR"
	;;
esac

info "Done. Run '$BINARY' to start, or '$BINARY -once' for a single snapshot."
