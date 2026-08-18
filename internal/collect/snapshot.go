// Package collect polls a llama.cpp server and derives live rates from its
// monotonic counters.
package collect

import (
	"time"

	"github.com/pefman/ltop/internal/gpu"
	"github.com/pefman/ltop/internal/host"
	"github.com/pefman/ltop/internal/llama"
)

// Snapshot is one fully-derived observation of the server and host.
type Snapshot struct {
	At      time.Time
	ScrapeR time.Duration

	Online bool
	Err    error

	// Loading reports that the server is up but still loading a model.
	Loading bool
	// NeedsAuth reports that the server rejected the request for lack of a key.
	NeedsAuth bool
	// HasMetrics and HasSlots report whether those endpoints answered. A
	// server started without --metrics or --slots is still online.
	HasMetrics bool
	HasSlots   bool
	// ModelUnmatched reports that /v1/models listed several models and none
	// could be tied to the loaded one, so its metadata is withheld.
	ModelUnmatched bool

	Props llama.Props
	Model llama.Model
	Slots []llama.Slot
	Raw   llama.Metrics
	Host  host.Stats
	GPUs  []gpu.Device

	// DecodeStepsPerSec is llama_decode() calls per wall second. It is the only
	// counter that advances while a request is still generating, so it is the
	// dashboard's live activity signal.
	DecodeStepsPerSec float64

	// DecodeTokensPerSec and PrefillTokensPerSec are exact throughput figures
	// measured from token and time counter deltas. llama.cpp only publishes
	// those counters when a request completes, so these hold the last measured
	// value and the matching age reports how stale it is.
	DecodeTokensPerSec  float64
	DecodeAge           time.Duration
	HasDecodeMeasured   bool
	PrefillTokensPerSec float64
	PrefillAge          time.Duration
	HasPrefillMeasured  bool

	// Generating reports that token counters advanced during this interval.
	Generating bool

	// ContextPressure is the fullest slot's context occupancy, in 0..1.
	ContextPressure float64
	// TokensPerJoule is decode throughput divided by total GPU power draw.
	TokensPerJoule float64
	HasEfficiency  bool

	// EnergyWh is GPU energy observed since ltop started, integrated from
	// successive power readings. It covers ltop's session, not the server's.
	EnergyWh  float64
	HasEnergy bool

	// Restarted reports that counters went backwards, meaning the server was
	// restarted and history before this point is not comparable.
	Restarted bool
	// ModelChanged reports that a different model is now loaded.
	ModelChanged bool
}

// History is a fixed-capacity ring of recent values for sparklines.
type History struct {
	values []float64
	cap    int
}

// NewHistory returns a ring holding at most n values.
func NewHistory(n int) *History { return &History{cap: n} }

// Push appends a value, evicting the oldest once full.
func (h *History) Push(v float64) {
	h.values = append(h.values, v)
	if len(h.values) > h.cap {
		h.values = h.values[len(h.values)-h.cap:]
	}
}

// Values returns the retained values, oldest first.
func (h *History) Values() []float64 { return h.values }

// Reset discards all retained values.
func (h *History) Reset() { h.values = h.values[:0] }

// Max returns the largest retained value, or 0 when empty.
func (h *History) Max() float64 {
	m := 0.0
	for _, v := range h.values {
		if v > m {
			m = v
		}
	}
	return m
}

// Last returns the most recent value, or 0 when empty.
func (h *History) Last() float64 {
	if len(h.values) == 0 {
		return 0
	}
	return h.values[len(h.values)-1]
}
