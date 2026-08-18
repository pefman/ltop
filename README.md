# ltop

**An htop for llama.cpp.** A terminal activity monitor that shows what your local LLM server is actually doing — live throughput, KV cache efficiency, speculative decoding quality, GPU load, and per-slot context usage, all on one screen.

[![CI](https://github.com/pefman/ltop/actions/workflows/ci.yml/badge.svg)](https://github.com/pefman/ltop/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/pefman/ltop)](https://github.com/pefman/ltop/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/pefman/ltop)](https://goreportcard.com/report/github.com/pefman/ltop)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

```
 ltop v0.1.0  Qwen3.8-27B-UD-Q4_K_XL  Q4_K - Small  27.32B  16.7GiB      ● online
  http://127.0.0.1:11436  ctx 140032/262144  llama.cpp b10430  up 4m12s  scrape 27ms

  GPU0      [||||||||||||||||||       ]    68%  23.4GiB of 24.0GiB  64°C  333/450W
    vram    [||||||||||||||||||||||||||] 97.5%  NVIDIA GeForce RTX 4090
  CPU       [|||||||||||              ]  43.8%  28 cores  load 9.05
  RAM       [|||||||||||||            ]  52.5%  32.8GiB of 62.5GiB

  DECODE       21.3 steps/s  ▂▃▅▇█▇▅▃▂▄▆█▇▅▃▂▁▃▅▇█▆▄▂
    tok/s      43.6 tok/s  measured 12s ago   lifetime 44.2
  PREFILL      1163 tok/s  measured 1m04s ago   lifetime 1161
  QUEUE     1 processing   0 deferred   1.00 slots/decode   0.131 tok/J

  KV CACHE  [|||||||||||||||||||||||  ]  93.0%  3.41M reused / 257.7k prefilled
  CONTEXT   [|||||||||                ]  36.9%  longest seen 140.0k tok

  SPEC      [||||||||||||||||         ]  64.4%  2.93x est speedup  1.93 accepted/draft
    draft   pos0 79%  ·  pos1 63%  ·  pos2 51%

  SLOT STATE    TASK          PROMPT     CACHED     CTX
  0    running  10050          51716      50750   36.9%  [||||        ]

  TOTALS    59.5k generated  1.13M prefilled  6.04M cached  21.4k decodes  ~1h16m saved

  q quit   p pause   +/- 1s   s spec   g gpu   r refresh   ? help
```

## Why

Most llama.cpp dashboards read `llamacpp:predicted_tokens_seconds` and call it throughput. That gauge is a **cumulative average since server start** — it barely moves. Worse, llama.cpp only publishes its token counters when a request *completes*, so anything dividing them by wall-clock time reads **zero during generation**, which is exactly when you're watching.

ltop handles this correctly. It shows a genuinely live activity signal derived from `n_decode_total` (the only counter that advances mid-generation) alongside exact measured throughput that is held and visibly aged rather than silently dropping to zero.

It also surfaces things nothing else does — like per-position speculative draft acceptance, which tells you how long your draft should actually be.

## Requirements

- Linux (ltop reads `/proc` and `/sys` directly)
- A [llama.cpp](https://github.com/ggml-org/llama.cpp) server

Start `llama-server` with introspection enabled:

```bash
llama-server -m model.gguf --slots --metrics
```

Both flags are optional. Without `--metrics` you lose the throughput, cache and speculative panels; without `--slots` you lose the slot table. ltop stays online either way and tells you which flag is missing.

GPU panels use `nvidia-smi` for NVIDIA and the `amdgpu` sysfs interface for AMD. Neither is required — the panel hides itself when no GPU is readable.

## Install

### From a release

Prebuilt `linux/amd64` and `linux/arm64` binaries are on the [releases page](https://github.com/pefman/ltop/releases).

```bash
curl -sSL https://github.com/pefman/ltop/releases/latest/download/ltop_linux_amd64.tar.gz | tar xz
install -Dm755 ltop ~/.local/bin/ltop
```

### From source

```bash
git clone https://github.com/pefman/ltop
cd ltop
./install.sh
```

The script checks your Go version, runs the tests, builds with the version stamped in, and installs to `~/.local/bin`.

### With go install

```bash
go install github.com/pefman/ltop@latest
```

## Usage

```bash
ltop                # the dashboard
ltop -once          # one plain-text snapshot, then exit
ltop -reconfigure   # pick a different server
ltop -version
```

On first run ltop scans localhost for llama.cpp servers, asks which to monitor, and saves your choice to `~/.config/ltop/config.json`. Servers started with `--api-key` are supported; ltop asks for the key only when the server actually demands one.

`ltop -once` prints plain text, so it pipes and greps cleanly:

```bash
ltop -once | grep 'hit rate'
```

### Keys

| Key | Action |
| --- | --- |
| `q` / `esc` | quit |
| `p` / `space` | pause polling |
| `+` / `-` | faster / slower poll interval |
| `s` | toggle the speculative decoding panel |
| `g` | toggle the GPU panel |
| `r` | force one refresh |
| `?` | help |

## What the metrics mean

| Metric | Meaning |
| --- | --- |
| **DECODE steps/s** | `llama_decode()` calls per second — the live activity signal, and what drives the sparkline. |
| **tok/s measured** | Exact throughput from token and time counters. Only refreshes when a request completes, so it is shown with its age and dims once stale. |
| **PREFILL** | Prompt processing throughput, same measurement rules. |
| **KV CACHE** | Share of prompt tokens reused instead of re-prefilled. A low rate on a chat workload is usually the biggest speedup available to you. |
| **CONTEXT** | The fullest slot's context occupancy — warns before eviction starts. |
| **SPEC** | Speculative draft acceptance with an estimated speedup. Below roughly 35% the draft model typically costs more than it saves. |
| **SPEC draft posN** | Share of drafts accepted at each depth. Where this falls off is your practical draft-length limit. |
| **QUEUE** | Requests processing and deferred, plus busy slots per decode — how well batching is working. |
| **tok/J** | Decode tokens per joule of GPU energy, for comparing quantisations and offload splits. Only reported while actively decoding. |
| **TOTALS** | Lifetime counters from the server: tokens generated, prefilled, reused from cache, and `llama_decode()` calls. Plus an estimate of the prefill wall time the cache avoided, and the GPU energy ltop has observed since it started. |

## Configuration

`~/.config/ltop/config.json`, created on first run and written `0600`:

```json
{
  "endpoint": "http://127.0.0.1:11436",
  "poll_interval_ms": 1000
}
```

The endpoint is not read from the environment. Run `ltop -reconfigure` to change it.

## Multiple models

Model metadata is matched against the loaded model rather than taken from the first entry of `/v1/models`. On a router such as `llama-swap` that serves several models, ltop withholds quantisation and size rather than pairing them with the wrong model's name. Model swaps are detected within ten seconds and clear the rate history so a fast model's numbers do not blend into a slow one's.

## Troubleshooting

**"server unreachable"** — check the endpoint with `curl http://host:port/health`. If the server needs an API key, run `ltop -reconfigure`.

**No throughput panels** — the server was started without `--metrics`.

**Empty slot table** — the server was started without `--slots`.

**tok/s says "awaiting a completed request"** — expected. llama.cpp only publishes token counters when a request finishes; the DECODE steps/s line and sparkline stay live in the meantime.

**No GPU panel** — `nvidia-smi` is not on `PATH`, or the AMD card does not expose `gpu_busy_percent` in sysfs.

## Development

```bash
make vet     # gofmt + go vet
make test    # go test ./...
make build
```

Tests cover the Prometheus parser against a captured `/metrics` fixture, the derived metrics, counter-reset rebaselining, degraded-endpoint handling, config persistence, and dashboard layout at several terminal widths.

Releases are cut by [GoReleaser](https://goreleaser.com) from a `v*` tag:

```bash
git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0
```

## License

[MIT](LICENSE)
