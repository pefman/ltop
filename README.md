# ltop

An htop-style terminal activity monitor for [llama.cpp](https://github.com/ggml-org/llama.cpp) servers. Linux only.

`ltop` polls a llama.cpp server's native telemetry endpoints once per second, derives live rates from its counters, and renders a single-screen dashboard with generation slots as the process list.

## Requirements

Start `llama-server` with slot and metrics introspection enabled:

```
llama-server -m model.gguf --slots --metrics
```

Both flags are optional. Without `--metrics` you lose the throughput, cache and
speculative panels; without `--slots` you lose the slot table. `ltop` stays
online either way and says which flag is missing.

Servers started with `--api-key` are supported; `ltop` asks for the key during
setup and stores it in the config file.

GPU panels use `nvidia-smi` for NVIDIA cards and the `amdgpu` sysfs interface for AMD. Neither is required — the panel is hidden when no GPU is readable.

## Install

```
make install     # builds and installs to ~/.local/bin
```

## Usage

```
ltop                # dashboard
ltop -once          # one plain-text snapshot, then exit
ltop -reconfigure   # pick a different server
```

On first run `ltop` scans localhost for llama.cpp servers, asks which to monitor, and saves the choice to `~/.config/ltop/config.json`. The endpoint is not read from the environment.

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

**`DECODE` steps/s** — `llama_decode()` calls per second. This is the only counter llama.cpp advances *while a request is still generating*, so it is the live activity signal, and it drives the sparkline.

**`tok/s` measured** — exact throughput computed from the token and generation-time counters. llama.cpp folds these into `/metrics` only when a request *completes*, so between completions the value is held and shown with its age, dimming once stale. A monitor that divides these counters by wall-clock time reads zero during generation; `ltop` does not.

**`KV CACHE`** — share of prompt tokens reused from the cache rather than re-prefilled. A low rate on a chat workload means prompt caching is not being hit, which is usually the single largest available speedup.

**`CONTEXT`** — the fullest slot's context occupancy, warning before eviction starts.

**`SPEC`** — speculative decoding acceptance rate, with an estimated speedup. Below roughly 35% a draft model typically costs more than it saves. The per-position row shows the share of drafts accepted at each depth; where it falls off is the practical limit for draft length.

**`tok/J`** — decode tokens per joule of GPU energy, for comparing quantisations and offload splits. Only reported while actively decoding.

**`QUEUE`** — requests processing and deferred, plus average busy slots per decode, which shows how well batching is being used.

## Multiple models

Model metadata is matched against the loaded model rather than taken from the
first entry of `/v1/models`. On a router such as `llama-swap` that serves
several models, `ltop` withholds quantisation and size rather than pairing them
with the wrong model's name. Model swaps are detected within ten seconds and
clear the rate history so a fast model's numbers do not blend into a slow one's.

## Development

```
make vet
make test
```

Tests cover the Prometheus parser against a captured `/metrics` fixture, the derived metrics, counter-reset rebaselining, config persistence, and dashboard layout at several terminal widths.
