package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pefman/ltop/internal/collect"
	"github.com/pefman/ltop/internal/config"
	"github.com/pefman/ltop/internal/format"
	"github.com/pefman/ltop/internal/gpu"
)

// runOnce prints a single plain-text snapshot. Two scrapes are taken because
// live rates are derived from the delta between them.
func runOnce(ctx context.Context, out io.Writer, cfg config.Config) error {
	c := collect.New(cfg.Endpoint, cfg.APIKey)

	snap := c.Poll(ctx)
	if snap.Online && snap.HasMetrics {
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
		snap = c.Poll(ctx)
	}
	if !snap.Online {
		return fmt.Errorf("%s unreachable: %w", cfg.Endpoint, snap.Err)
	}

	printSnapshot(out, cfg.Endpoint, snap)
	return nil
}

func printSnapshot(out io.Writer, endpoint string, s collect.Snapshot) {
	p, m := s.Props, s.Raw

	section(out, "server")
	kv(out, "endpoint", endpoint)
	kv(out, "build", p.BuildInfo)
	kv(out, "scrape", s.ScrapeR.Round(time.Microsecond).String())
	kv(out, "sleeping", fmt.Sprint(p.IsSleeping))
	kv(out, "metrics", available(s.HasMetrics, "start llama-server with --metrics"))
	kv(out, "slots", available(s.HasSlots, "start llama-server with --slots"))
	if s.Loading {
		kv(out, "state", "loading model")
	}

	section(out, "model")
	kv(out, "name", p.ModelName())
	if s.ModelUnmatched {
		kv(out, "metadata", "withheld; endpoint serves several models")
	} else if s.Model.ID != "" {
		meta := s.Model.Meta
		kv(out, "id", s.Model.ID)
		kv(out, "quant", meta.FType)
		kv(out, "params", format.Count(float64(meta.NParams)))
		kv(out, "size", format.Bytes(uint64(meta.Size)))
		kv(out, "context", fmt.Sprintf("%d of %d trained", meta.NCtx, meta.NCtxTrain))
	}

	if !s.HasMetrics {
		printHost(out, s)
		fmt.Fprintln(out)
		return
	}

	section(out, "throughput")
	kv(out, "decode steps/s (live)", fmt.Sprintf("%.2f", s.DecodeStepsPerSec))
	kv(out, "decode measured", measured(s.DecodeTokensPerSec, s.DecodeAge, s.HasDecodeMeasured))
	kv(out, "prefill measured", measured(s.PrefillTokensPerSec, s.PrefillAge, s.HasPrefillMeasured))
	kv(out, "decode lifetime", format.Rate(m.LifetimePredictedTokensPerSec())+" tok/s")
	kv(out, "prefill lifetime", format.Rate(m.LifetimePromptTokensPerSec())+" tok/s")
	kv(out, "tokens generated", format.Count(m.PredictedTokensTotal))
	kv(out, "decode calls", format.Count(m.DecodeTotal))

	section(out, "cache")
	kv(out, "hit rate", format.Percent(m.CacheHitRate()))
	kv(out, "reused", format.Count(m.PromptCachedTotal)+" tok")
	kv(out, "prefilled", format.Count(m.PromptTokensTotal)+" tok")
	kv(out, "longest sequence", format.Count(m.TokensMax)+" tok")

	section(out, "queue")
	kv(out, "processing", fmt.Sprintf("%.0f", m.RequestsProcessing))
	kv(out, "deferred", fmt.Sprintf("%.0f", m.RequestsDeferred))
	kv(out, "busy slots per decode", fmt.Sprintf("%.2f", m.BusySlotsPerDecode))
	kv(out, "context pressure", format.Percent(s.ContextPressure))

	if m.SpecEnabled() {
		section(out, "speculative decoding")
		kv(out, "acceptance", format.Percent(m.SpecAcceptanceRate()))
		kv(out, "accepted per draft", fmt.Sprintf("%.2f", m.SpecTokensPerDraft()))
		kv(out, "estimated speedup", fmt.Sprintf("%.2fx", m.SpecSpeedup()))
		kv(out, "drafted", format.Count(m.SpecDraftTokensTotal)+" tok")
		for i, v := range m.SpecAcceptedPerPos {
			share := 0.0
			if m.SpecDraftsTotal > 0 {
				share = v / m.SpecDraftsTotal
			}
			kv(out, fmt.Sprintf("  position %d", i), fmt.Sprintf("%s (%s of drafts)", format.Count(v), format.Percent(share)))
		}
	}

	if len(s.Slots) > 0 {
		section(out, "slots")
		fmt.Fprintf(out, "  %-4s %-10s %-8s %10s %10s %8s\n", "ID", "STATE", "TASK", "PROMPT", "CACHED", "CTX")
		for _, sl := range s.Slots {
			state := "idle"
			if sl.IsProcessing {
				state = "running"
			}
			fmt.Fprintf(out, "  %-4d %-10s %-8d %10d %10d %8s\n",
				sl.ID, state, sl.TaskID, sl.PromptTokens, sl.PromptCached, format.Percent(sl.ContextUsed()))
		}
	}

	section(out, "host")
	kv(out, "cpu", fmt.Sprintf("%.1f%% over %d cores", s.Host.CPUPercent, s.Host.CPUCores))
	kv(out, "memory", fmt.Sprintf("%s of %s (%s)",
		format.Bytes(s.Host.MemUsedBytes()), format.Bytes(s.Host.MemTotalBytes), format.Percent(s.Host.MemPercent())))
	kv(out, "load", fmt.Sprintf("%.2f", s.Host.LoadAvg1))

	if len(s.GPUs) > 0 {
		section(out, "gpu")
		for _, d := range s.GPUs {
			printGPU(out, d)
		}
		if s.HasEfficiency {
			kv(out, "efficiency", fmt.Sprintf("%.3f tok/J", s.TokensPerJoule))
		}
	}
	fmt.Fprintln(out)
}

func printHost(out io.Writer, s collect.Snapshot) {
	section(out, "host")
	kv(out, "cpu", fmt.Sprintf("%.1f%% over %d cores", s.Host.CPUPercent, s.Host.CPUCores))
	kv(out, "memory", fmt.Sprintf("%s of %s (%s)",
		format.Bytes(s.Host.MemUsedBytes()), format.Bytes(s.Host.MemTotalBytes), format.Percent(s.Host.MemPercent())))
	kv(out, "load", fmt.Sprintf("%.2f", s.Host.LoadAvg1))

	if len(s.GPUs) > 0 {
		section(out, "gpu")
		for _, d := range s.GPUs {
			printGPU(out, d)
		}
	}
}

func available(ok bool, hint string) string {
	if ok {
		return "available"
	}
	return "unavailable; " + hint
}

func printGPU(out io.Writer, d gpu.Device) {
	fmt.Fprintf(out, "  [%d] %s (%s)\n", d.Index, d.Name, d.Vendor)
	if d.HasUtil {
		kv(out, "  util", fmt.Sprintf("%.0f%%", d.UtilPercent))
	}
	if d.HasMem {
		kv(out, "  vram", fmt.Sprintf("%s of %s (%s)",
			format.Bytes(d.MemUsedBytes), format.Bytes(d.MemTotalBytes), format.Percent(d.MemPercent())))
	}
	if d.HasTemp {
		kv(out, "  temp", fmt.Sprintf("%.0fC", d.TempCelsius))
	}
	if d.HasPower {
		kv(out, "  power", fmt.Sprintf("%.0fW of %.0fW", d.PowerWatts, d.PowerLimitWatts))
	}
}

func section(out io.Writer, title string) {
	fmt.Fprintf(out, "\n%s\n", strings.ToUpper(title))
}

func measured(rate float64, age time.Duration, ok bool) string {
	if !ok {
		return "awaiting a completed request"
	}
	return fmt.Sprintf("%s tok/s (%s ago)", format.Rate(rate), format.Duration(age))
}

func kv(out io.Writer, k, v string) {
	if v == "" {
		v = "-"
	}
	fmt.Fprintf(out, "  %-24s %s\n", k, v)
}
