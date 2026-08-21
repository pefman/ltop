# AGENTS

ltop is a Linux TUI activity monitor for llama.cpp. Release builds can
self-update from GitHub. That path is a frozen protocol: a change that
looks harmless in this repo can strand every already-shipped `v0.2.0+`
binary.

## Self-update protocol v1 — do not break

Every `v0.2.0+` binary looks up **latest**, not a specific tag:

```
https://github.com/pefman/ltop/releases/latest/download/
```

One renamed asset on a future release breaks every older updater.

### Every `v*` tag MUST publish these exact names

| Asset | Role |
| --- | --- |
| `update.json` | primary: version + SHA-256 |
| `checksums.txt` | fallback if `update.json` is missing |
| `ltop_linux_amd64.tar.gz` | frozen archive name |
| `ltop_linux_arm64.tar.gz` | same for arm64 |

### Do not change

- Those four names (no version in the tarball filename).
- The GitHub repo path `pefman/ltop` without leaving redirects (self-update hardcodes that URL).
- Archive layout: gzipped tar, **top-level file named `ltop`**, ELF, Linux amd64 + arm64.
- `update.json` v1 keys: `schema`, `version` (`X.Y.Z`), `binary` (`"ltop"`), `assets["linux/amd64"]` and `assets["linux/arm64"]` each with `name` + 64-char hex `sha256` of the **archive bytes** (not the inner binary).
- Stamping `internal/buildinfo.Version` from the tag (`0.2.0`, never `dev` for a release). Dirty / `git describe` builds do not self-update.
- The release workflow step that runs `genmanifest` and uploads `update.json`. GoReleaser does not publish that file by itself.
- Tag shape: `v*` so `.github/workflows/release.yml` fires.
- Never open the running binary with `O_WRONLY`. Linux returns `ETXTBSY`
  ("text file busy"). Replace by writing a sibling `ltop.new` and renaming
  the live inode aside (`ltop` → `ltop.bak`, then `ltop.new` → `ltop`).
  `v0.2.0`/`v0.2.1` still have the old check and cannot `u` themselves
  onto a later build; those need a one-time manual install.

The original GitHub **v0.1.0** binary cannot self-update (no updater). From
**v0.2.2** onward, breaking the list above is what bricks upgrades.

Canonical comments live in `internal/update/protocol.go`. CI pins the GoReleaser name templates in `internal/update/contract_test.go`. `genmanifest` refuses to upload if either Linux arch is missing.

### Safe

- Extra files in the tarball (README, LICENSE, docs) as long as `ltop` stays at the top level.
- Extra GitHub release assets besides the four frozen names.
- **New JSON fields** on `update.json`. Old clients ignore unknown keys.
- Bumping `"schema": 2` later, **if** the v1 keys remain.
- More GOOS/GOARCH later, **and** still shipping the two Linux archives under the old names.
- Ordinary code, TUI, and dependency changes.

### How to cut a release

```bash
git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z
```

Wait for the **release** workflow: GoReleaser **and** `Publish update.json` both green. Then check

`https://github.com/pefman/ltop/releases/latest/download/update.json`

has `"version": "X.Y.Z"` and both `linux/amd64` and `linux/arm64` hashes.
