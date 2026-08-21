package collect

import (
	"context"
	"errors"
	"maps"
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

	lastModel           string
	modelUnmatched      bool
	lastDecodeRate      float64
	lastPrefillRate     float64
	lastDecodeAt        time.Time
	lastPrefillAt       time.Time
	energyWh            float64
	energyByGPU         map[int]float64
	lastEnergyAt        time.Time
	baseline            llama.Metrics
	baselineSet         bool
	baselineAt          time.Time
	baselineEnergy      float64
	baselineEnergyByGPU map[int]float64

	DecodeHist  *History
	PrefillHist *History
	StepHist    *History
}

// New returns a collector for the given endpoint.
func New(endpoint, apiKey string) *Collector {
	return &Collector{
		client:      llama.New(endpoint, apiKey),
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

// ResetStats rebases the totals onto the current counter values.
//
// llama.cpp offers no way to zero its own counters, so ltop records where they
// stand and reports the difference from here on, letting a single workload be
// measured without restarting the server.
func (c *Collector) ResetStats() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.baseline, c.baselineSet, c.baselineAt = c.prev, c.havePrev, time.Now()
	c.baselineEnergy = c.energyWh
	c.baselineEnergyByGPU = maps.Clone(c.energyByGPU)
	// prev is deliberately kept: rates and restart detection compare against
	// the server's absolute counters and are unaffected by the display window.
	c.clearHistory()
}

// Endpoint returns the server root being polled.
func (c *Collector) Endpoint() string { return c.client.BaseURL() }

// HasGPU reports whether any GPU backend was detected.
func (c *Collector) HasGPU() bool { return c.gpus.Available() }

// Poll performs one scrape and returns a derived snapshot. A snapshot is
// always returned, with Online false and Err set when the server is
// unreachable, so the caller can render an error state rather than exit.
//
// Liveness comes from /health alone. A server started without --metrics or
// --slots is still online and still worth showing; those panels degrade
// individually rather than blanking the dashboard.
func (c *Collector) Poll(ctx context.Context) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	snap := Snapshot{At: start}

	// Host and GPU are sampled regardless of server reachability so the
	// dashboard stays useful while llama.cpp is restarting.
	snap.Host = c.hostS.Sample()
	snap.GPUs = c.gpus.Sample(ctx)
	c.accumulateEnergy(&snap, start)

	health, err := c.client.Health(ctx)
	snap.ScrapeR = time.Since(start)
	if err != nil {
		snap.Err = err
		snap.NeedsAuth = errors.Is(err, llama.ErrUnauthorized)
		return snap
	}
	snap.Online = true
	snap.Loading = health.Loading()

	var absolute llama.Metrics
	if set, err := c.client.Metrics(ctx); err == nil {
		absolute = llama.ParseMetrics(set)
		snap.Raw = absolute
		snap.HasMetrics = true
		if c.baselineSet {
			snap.Raw = absolute.Since(c.baseline)
			snap.StatsReset = true
			snap.StatsSince = start.Sub(c.baselineAt)
		}
	} else if !errors.Is(err, llama.ErrEndpointDisabled) {
		snap.Err = err
	}

	c.refreshProps(ctx, start, &snap)
	snap.Props = c.props
	snap.Model = c.model
	snap.ModelUnmatched = c.modelUnmatched

	if slots, err := c.client.Slots(ctx); err == nil {
		snap.Slots = slots
		snap.HasSlots = true
	} else if !errors.Is(err, llama.ErrEndpointDisabled) {
		snap.Err = err
	}
	snap.ContextPressure = contextPressure(snap.Slots)

	if snap.HasMetrics {
		c.deriveRates(&snap, start, absolute)
		deriveEfficiency(&snap)

		c.DecodeHist.Push(snap.DecodeTokensPerSec)
		c.PrefillHist.Push(snap.PrefillTokensPerSec)
		c.StepHist.Push(snap.DecodeStepsPerSec)

		c.prev, c.prevAt, c.havePrev = absolute, start, true
	}
	snap.ScrapeR = time.Since(start)
	return snap
}

// deriveRates computes the live decode-step rate and, when llama.cpp publishes
// fresh token counters, the exact measured throughput.
//
// llama.cpp only folds a request's token and time totals into /metrics once the
// request finishes. Between completions those counters are frozen, so the
// measured tok/s is carried forward with an age rather than falling to zero,
// and n_decode_total supplies the live activity signal in the meantime.
func (c *Collector) deriveRates(snap *Snapshot, now time.Time, cur llama.Metrics) {
	if !c.havePrev {
		return
	}

	if counterWentBackwards(c.prev, cur) {
		snap.Restarted = true
		// A restart zeroes the server's counters, so any baseline taken
		// against the old ones would make every total negative.
		c.baselineSet = false
		snap.StatsReset = false
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
	c.clearHistory()
	c.havePrev = false
}

// clearHistory drops the sparklines and held rates without discarding the
// counter baseline that rate derivation depends on.
func (c *Collector) clearHistory() {
	c.DecodeHist.Reset()
	c.PrefillHist.Reset()
	c.StepHist.Reset()
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

	if models, err := c.client.Models(ctx); err == nil {
		model, ok := llama.MatchModel(models, props)
		c.model, c.modelUnmatched = model, !ok && len(models) > 0
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

// accumulateEnergy integrates GPU power draw over the interval between polls.
func (c *Collector) accumulateEnergy(snap *Snapshot, now time.Time) {
	watts := 0.0
	readable := false
	for _, d := range snap.GPUs {
		if d.HasPower {
			watts += d.PowerWatts
			readable = true
		}
	}
	if !readable {
		return
	}
	if c.energyByGPU == nil {
		c.energyByGPU = make(map[int]float64)
	}
	if !c.lastEnergyAt.IsZero() {
		hours := now.Sub(c.lastEnergyAt).Hours()
		// Guard against a suspended laptop reporting an enormous interval.
		if hours > 0 && hours < 1 {
			c.energyWh += watts * hours
			for _, d := range snap.GPUs {
				if d.HasPower {
					c.energyByGPU[d.Index] += d.PowerWatts * hours
				}
			}
		}
	}
	c.lastEnergyAt = now
	snap.EnergyWh, snap.HasEnergy = c.energyWh-c.baselineEnergy, true
	snap.GPUEnergyWh = make(map[int]float64, len(c.energyByGPU))
	for idx, wh := range c.energyByGPU {
		snap.GPUEnergyWh[idx] = wh - c.baselineEnergyByGPU[idx]
	}
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
