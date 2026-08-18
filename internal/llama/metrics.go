package llama

import (
	"strconv"
	"strings"
	"time"

	"github.com/pefman/ltop/internal/promparse"
)

func atoiLabel(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

// Metric names exported by llama.cpp's /metrics handler.
const (
	mPromptTokens        = "llamacpp:prompt_tokens_total"
	mPromptCached        = "llamacpp:prompt_tokens_cached_total"
	mPromptSeconds       = "llamacpp:prompt_seconds_total"
	mPredictedTokens     = "llamacpp:tokens_predicted_total"
	mPredictedSeconds    = "llamacpp:tokens_predicted_seconds_total"
	mDecodeTotal         = "llamacpp:n_decode_total"
	mTokensMax           = "llamacpp:n_tokens_max"
	mSpecDraftTokens     = "llamacpp:spec_decode_num_draft_tokens_total"
	mSpecAcceptedTokens  = "llamacpp:spec_decode_num_accepted_tokens_total"
	mSpecDrafts          = "llamacpp:spec_decode_num_drafts_total"
	mSpecAcceptedPerPos  = "llamacpp:spec_decode_num_accepted_tokens_per_pos_total"
	mPromptTokensPerSec  = "llamacpp:prompt_tokens_seconds"
	mPredictTokensPerSec = "llamacpp:predicted_tokens_seconds"
	mRequestsProcessing  = "llamacpp:requests_processing"
	mRequestsDeferred    = "llamacpp:requests_deferred"
	mBusySlotsPerDecode  = "llamacpp:n_busy_slots_per_decode"
)

// Metrics is a typed snapshot of one /metrics scrape.
//
// The *Total fields are monotonic counters; rates must be derived from
// successive snapshots. The AvgPromptTokensPerSec and AvgPredictedTokensPerSec
// gauges are cumulative averages over the server's whole lifetime, so they are
// reported as lifetime figures and never used as live throughput.
type Metrics struct {
	PromptTokensTotal     float64
	PromptCachedTotal     float64
	PromptSecondsTotal    float64
	PredictedTokensTotal  float64
	PredictedSecondsTotal float64
	DecodeTotal           float64
	TokensMax             float64

	SpecDraftTokensTotal    float64
	SpecAcceptedTokensTotal float64
	SpecDraftsTotal         float64
	// SpecAcceptedPerPos is indexed by draft position; entry i counts drafts
	// accepted at depth i, which reveals the useful draft length.
	SpecAcceptedPerPos []float64

	AvgPromptTokensPerSec    float64
	AvgPredictedTokensPerSec float64

	RequestsProcessing float64
	RequestsDeferred   float64
	BusySlotsPerDecode float64
}

// ParseMetrics projects a parsed exposition document into typed fields.
func ParseMetrics(set promparse.Set) Metrics {
	m := Metrics{
		PromptTokensTotal:     set.ValueOr(mPromptTokens, 0),
		PromptCachedTotal:     set.ValueOr(mPromptCached, 0),
		PromptSecondsTotal:    set.ValueOr(mPromptSeconds, 0),
		PredictedTokensTotal:  set.ValueOr(mPredictedTokens, 0),
		PredictedSecondsTotal: set.ValueOr(mPredictedSeconds, 0),
		DecodeTotal:           set.ValueOr(mDecodeTotal, 0),
		TokensMax:             set.ValueOr(mTokensMax, 0),

		SpecDraftTokensTotal:    set.ValueOr(mSpecDraftTokens, 0),
		SpecAcceptedTokensTotal: set.ValueOr(mSpecAcceptedTokens, 0),
		SpecDraftsTotal:         set.ValueOr(mSpecDrafts, 0),
		SpecAcceptedPerPos:      acceptedPerPos(set),

		AvgPromptTokensPerSec:    set.ValueOr(mPromptTokensPerSec, 0),
		AvgPredictedTokensPerSec: set.ValueOr(mPredictTokensPerSec, 0),

		RequestsProcessing: set.ValueOr(mRequestsProcessing, 0),
		RequestsDeferred:   set.ValueOr(mRequestsDeferred, 0),
		BusySlotsPerDecode: set.ValueOr(mBusySlotsPerDecode, 0),
	}
	return m
}

// acceptedPerPos flattens the position-labeled histogram into a dense slice.
func acceptedPerPos(set promparse.Set) []float64 {
	samples := set.Samples(mSpecAcceptedPerPos)
	if len(samples) == 0 {
		return nil
	}
	byPos := make(map[int]float64, len(samples))
	maxPos := -1
	for _, s := range samples {
		pos, err := atoiLabel(s.Labels["position"])
		if err != nil {
			continue
		}
		byPos[pos] = s.Value
		if pos > maxPos {
			maxPos = pos
		}
	}
	if maxPos < 0 {
		return nil
	}
	out := make([]float64, maxPos+1)
	for pos, v := range byPos {
		out[pos] = v
	}
	return out
}

// SpecEnabled reports whether the server produced speculative decoding counters.
func (m Metrics) SpecEnabled() bool { return m.SpecDraftTokensTotal > 0 }

// SpecAcceptanceRate is the lifetime share of draft tokens the target model
// accepted, in 0..1. Below roughly 0.4 the draft model usually costs more than
// it saves.
func (m Metrics) SpecAcceptanceRate() float64 {
	return safeDiv(m.SpecAcceptedTokensTotal, m.SpecDraftTokensTotal)
}

// SpecTokensPerDraft is the mean number of accepted tokens per verification
// step. Adding the always-emitted target token gives the effective speedup.
func (m Metrics) SpecTokensPerDraft() float64 {
	return safeDiv(m.SpecAcceptedTokensTotal, m.SpecDraftsTotal)
}

// SpecSpeedup estimates the decode speedup versus running without a draft model.
func (m Metrics) SpecSpeedup() float64 {
	if m.SpecDraftsTotal <= 0 {
		return 1
	}
	return m.SpecTokensPerDraft() + 1
}

// CacheHitRate is the share of prompt tokens served from the KV cache rather
// than re-prefilled, in 0..1.
func (m Metrics) CacheHitRate() float64 {
	return safeDiv(m.PromptCachedTotal, m.PromptCachedTotal+m.PromptTokensTotal)
}

// LifetimePromptTokensPerSec is mean prefill throughput since server start.
func (m Metrics) LifetimePromptTokensPerSec() float64 {
	return safeDiv(m.PromptTokensTotal, m.PromptSecondsTotal)
}

// LifetimePredictedTokensPerSec is mean decode throughput since server start.
func (m Metrics) LifetimePredictedTokensPerSec() float64 {
	return safeDiv(m.PredictedTokensTotal, m.PredictedSecondsTotal)
}

// PrefillTimeSaved estimates the wall time the KV cache avoided, by valuing
// every reused token at the server's own mean prefill rate. It is an estimate:
// cached tokens would not necessarily have been prefilled at exactly that rate.
func (m Metrics) PrefillTimeSaved() time.Duration {
	rate := m.LifetimePromptTokensPerSec()
	if rate <= 0 || m.PromptCachedTotal <= 0 {
		return 0
	}
	return time.Duration(m.PromptCachedTotal / rate * float64(time.Second))
}

func safeDiv(a, b float64) float64 {
	if b <= 0 {
		return 0
	}
	return a / b
}
