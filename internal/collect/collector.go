package collect

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/pefman/ltop/internal/gpu"
	"github.com/pefman/ltop/internal/host"
	"github.com/pefman/ltop/internal/llama"
)

// propsInterval limits how often /props is re-read; it is static apart from
// model swaps and sleep transitions.
const propsInterval = 10 * time.Second

// HistorySize is the number of samples retained for sparklines.
const HistorySize = 240

// Collector polls one llama.cpp server and maintains derived state.
type Collector struct {
	client *llama.Client
	gpus   *gpu.Multi
	hostS  *host.Sampler

	mu       sync.Mutex
	prev     llama.Metrics
	prevAt   time.Time
	havePrev bool
	props    llama.Props
	model    llama.Model
	propsAt  time.Time
	startAt  time.Time

	lastModel       string
	lastDecodeRate  float64
	lastPrefillRate float64
	lastDecodeAt    time.Time
	lastPrefillAt   time.Time

	DecodeHist  *History
	PrefillHist *History
	StepHist    *History
}

// New returns a collector for the given endpoint.
func New(endpoint string) *Collector {
	return &Collector{
		client:      llama.New(endpoint),
		gpus:        gpu.NewMulti(),
		hostS:       host.NewSampler(),
		startAt:     time.Now(),
		DecodeHist:  NewHistory(HistorySize),
		PrefillHist: NewHistory(HistorySize),
		StepHist:    NewHistory(HistorySize),
	}
}

// Uptime is how long ltop has been observing this server.
func (c *Collector) Uptime() time.Duration { return time.Since(c.startAt) }

// Endpoint returns the server root being polled.
func (c *Collector) Endpoint() string { return c.client.BaseURL() }

// HasGPU reports whether any GPU backend was detected.
func (c *Collector) HasGPU() bool { return c.gpus.Available() }

// Poll performs one scrape and returns a derived snapshot. A snapshot is
// always returned, with Online false and Err set when the server is
// unreachable, so the caller can render an error state rather than exit.
func (c *Collector) Poll(ctx context.Context) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	snap := Snapshot{At: start}

	// Host and GPU are sampled regardless of server reachability so the
	// dashboard stays useful while llama.cpp is restarting.
	snap.Host = c.hostS.Sample()
	snap.GPUs = c.gpus.Sample(ctx)

	set, err := c.client.Metrics(ctx)
	snap.ScrapeR = time.Since(start)
	if err != nil {
		snap.Err = err
		return snap
	}
	snap.Online = true
	snap.Raw = llama.ParseMetrics(set)

	c.refreshProps(ctx, start, &snap)
	snap.Props = c.props
	snap.Model = c.model

	if slots, err := c.client.Slots(ctx); err == nil {
		snap.Slots = slots
	} else if !errors.Is(err, llama.ErrSlotsDisabled) {
		snap.Err = err
	}
	snap.ContextPressure = contextPressure(snap.Slots)

	c.deriveRates(&snap, start)
	deriveEfficiency(&snap)

	c.DecodeHist.Push(snap.DecodeTokensPerSec)
	c.PrefillHist.Push(snap.PrefillTokensPerSec)
	c.StepHist.Push(snap.DecodeStepsPerSec)

	c.prev, c.prevAt, c.havePrev = snap.Raw, start, true
	return snap
}

// deriveRates computes the live decode-step rate and, when llama.cpp publishes
// fresh token counters, the exact measured throughput.
//
// llama.cpp only folds a request's token and time totals into /metrics once the
// request finishes. Between completions those counters are frozen, so the
// measured tok/s is carried forward with an age rather than falling to zero,
// and n_decode_total supplies the live activity signal in the meantime.
func (c *Collector) deriveRates(snap *Snapshot, now time.Time) {
	if !c.havePrev {
		return
	}

	cur := snap.Raw
	if counterWentBackwards(c.prev, cur) {
		snap.Restarted = true
		c.reset()
		return
	}

	wall := now.Sub(c.prevAt).Seconds()
	if wall <= 0 {
		return
	}

	if steps := cur.DecodeTotal - c.prev.DecodeTotal; steps >= 0 {
		snap.DecodeStepsPerSec = steps / wall
	}

	// Dividing tokens by the server's own busy seconds rather than by wall time
	// yields the rate achieved while generating, matching llama.cpp's timings.
	decodeTokens := cur.PredictedTokensTotal - c.prev.PredictedTokensTotal
	decodeSecs := cur.PredictedSecondsTotal - c.prev.PredictedSecondsTotal
	if decodeTokens > 0 && decodeSecs > 0 {
		c.lastDecodeRate = decodeTokens / decodeSecs
		c.lastDecodeAt = now
		snap.Generating = true
	}

	prefillTokens := cur.PromptTokensTotal - c.prev.PromptTokensTotal
	prefillSecs := cur.PromptSecondsTotal - c.prev.PromptSecondsTotal
	if prefillTokens > 0 && prefillSecs > 0 {
		c.lastPrefillRate = prefillTokens / prefillSecs
		c.lastPrefillAt = now
	}

	if !c.lastDecodeAt.IsZero() {
		snap.DecodeTokensPerSec = c.lastDecodeRate
		snap.DecodeAge = now.Sub(c.lastDecodeAt)
		snap.HasDecodeMeasured = true
	}
	if !c.lastPrefillAt.IsZero() {
		snap.PrefillTokensPerSec = c.lastPrefillRate
		snap.PrefillAge = now.Sub(c.lastPrefillAt)
		snap.HasPrefillMeasured = true
	}
}

func (c *Collector) reset() {
	c.DecodeHist.Reset()
	c.PrefillHist.Reset()
	c.StepHist.Reset()
	c.havePrev = false
	c.lastDecodeAt = time.Time{}
	c.lastPrefillAt = time.Time{}
	c.lastDecodeRate = 0
	c.lastPrefillRate = 0
}

func (c *Collector) refreshProps(ctx context.Context, now time.Time, snap *Snapshot) {
	if !c.propsAt.IsZero() && now.Sub(c.propsAt) < propsInterval {
		return
	}
	props, err := c.client.Props(ctx)
	if err != nil {
		return
	}
	c.propsAt = now

	if c.lastModel != "" && props.ModelPath != c.lastModel {
		snap.ModelChanged = true
		c.reset()
	}
	c.lastModel = props.ModelPath
	c.props = props

	if models, err := c.client.Models(ctx); err == nil && len(models) > 0 {
		c.model = models[0]
	}
}

// counterWentBackwards detects a server restart, which resets all counters.
func counterWentBackwards(prev, cur llama.Metrics) bool {
	return cur.PredictedTokensTotal < prev.PredictedTokensTotal ||
		cur.PromptTokensTotal < prev.PromptTokensTotal ||
		cur.DecodeTotal < prev.DecodeTotal
}

func contextPressure(slots []llama.Slot) float64 {
	worst := 0.0
	for _, s := range slots {
		if u := s.ContextUsed(); u > worst {
			worst = u
		}
	}
	return worst
}

// deriveEfficiency computes tokens generated per joule of GPU energy.
//
// It is only meaningful while the server is actually decoding: once generation
// stops, GPU power falls to idle while the last measured token rate persists,
// which would otherwise produce a steadily rising and meaningless figure.
func deriveEfficiency(snap *Snapshot) {
	const staleAfter = 5 * time.Second
	if snap.DecodeStepsPerSec <= 0 || !snap.HasDecodeMeasured || snap.DecodeAge > staleAfter {
		return
	}

	watts := 0.0
	for _, d := range snap.GPUs {
		if d.HasPower {
			watts += d.PowerWatts
		}
	}
	if watts <= 0 || snap.DecodeTokensPerSec <= 0 {
		return
	}
	snap.TokensPerJoule = snap.DecodeTokensPerSec / watts
	snap.HasEfficiency = true
}
