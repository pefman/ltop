package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pefman/ltop/internal/gpu"
	"github.com/pefman/ltop/internal/llama"
)

// fakeServer serves the llama.cpp endpoints ltop reads, with counters the test
// controls.
type fakeServer struct {
	*httptest.Server
	metrics   string
	slots     []llama.Slot
	modelPath string
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{modelPath: "/models/a.gguf"}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, f.metrics)
	})
	mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(llama.Props{ModelPath: f.modelPath, BuildInfo: "test"})
	})
	mux.HandleFunc("/slots", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.slots)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"m","meta":{"n_ctx":4096}}]}`)
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) setCounters(decode, tokens, tokSecs, prompt, promptSecs float64) {
	f.metrics = fmt.Sprintf(
		"llamacpp:n_decode_total %f\n"+
			"llamacpp:tokens_predicted_total %f\n"+
			"llamacpp:tokens_predicted_seconds_total %f\n"+
			"llamacpp:prompt_tokens_total %f\n"+
			"llamacpp:prompt_seconds_total %f\n",
		decode, tokens, tokSecs, prompt, promptSecs)
}

// The first poll cannot produce a rate because rates come from deltas.
func TestFirstPollHasNoRates(t *testing.T) {
	f := newFakeServer(t)
	f.setCounters(100, 1000, 10, 500, 1)

	c := New(f.URL)
	snap := c.Poll(context.Background())

	if !snap.Online {
		t.Fatalf("offline: %v", snap.Err)
	}
	if snap.DecodeStepsPerSec != 0 {
		t.Errorf("DecodeStepsPerSec = %v, want 0", snap.DecodeStepsPerSec)
	}
	if snap.HasDecodeMeasured {
		t.Error("HasDecodeMeasured = true on first poll")
	}
}

// Token counters only move when a request completes; the measured rate must be
// held with a growing age rather than dropping to zero.
func TestMeasuredRateIsStickyAndAges(t *testing.T) {
	f := newFakeServer(t)
	c := New(f.URL)
	ctx := context.Background()

	f.setCounters(100, 1000, 10, 500, 1)
	c.Poll(ctx)

	// 200 tokens generated in 4 seconds of decode time = 50 tok/s.
	f.setCounters(120, 1200, 14, 500, 1)
	snap := c.Poll(ctx)

	if !snap.HasDecodeMeasured {
		t.Fatal("HasDecodeMeasured = false after counters advanced")
	}
	if got := snap.DecodeTokensPerSec; got != 50 {
		t.Errorf("DecodeTokensPerSec = %v, want 50", got)
	}
	if !snap.Generating {
		t.Error("Generating = false while token counters advanced")
	}

	// Counters freeze, as they do mid-generation.
	f.setCounters(140, 1200, 14, 500, 1)
	snap = c.Poll(ctx)

	if got := snap.DecodeTokensPerSec; got != 50 {
		t.Errorf("rate not held: got %v, want 50", got)
	}
	if snap.DecodeAge <= 0 {
		t.Error("DecodeAge did not advance while counters were frozen")
	}
	if snap.Generating {
		t.Error("Generating = true while token counters were frozen")
	}
	if snap.DecodeStepsPerSec <= 0 {
		t.Error("DecodeStepsPerSec = 0 although n_decode_total advanced")
	}
}

// A restarted server resets its counters; ltop must rebaseline instead of
// reporting a negative rate.
func TestCounterResetRebaselines(t *testing.T) {
	f := newFakeServer(t)
	c := New(f.URL)
	ctx := context.Background()

	f.setCounters(100, 1000, 10, 500, 1)
	c.Poll(ctx)
	f.setCounters(120, 1200, 14, 600, 2)
	c.Poll(ctx)

	if len(c.StepHist.Values()) == 0 {
		t.Fatal("no history recorded before restart")
	}

	f.setCounters(2, 5, 0.1, 3, 0.1)
	snap := c.Poll(ctx)

	if !snap.Restarted {
		t.Error("Restarted = false after counters went backwards")
	}
	if snap.DecodeStepsPerSec < 0 || snap.DecodeTokensPerSec < 0 {
		t.Errorf("negative rate after restart: steps=%v tokens=%v",
			snap.DecodeStepsPerSec, snap.DecodeTokensPerSec)
	}
	if len(c.StepHist.Values()) != 1 {
		t.Errorf("history not cleared on restart: %d entries", len(c.StepHist.Values()))
	}
}

func TestOfflineServerYieldsSnapshotNotPanic(t *testing.T) {
	c := New("http://127.0.0.1:1")
	snap := c.Poll(context.Background())

	if snap.Online {
		t.Error("Online = true for an unreachable server")
	}
	if snap.Err == nil {
		t.Error("Err = nil for an unreachable server")
	}
}

func TestContextPressureUsesFullestSlot(t *testing.T) {
	slots := []llama.Slot{
		{NCtx: 1000, PromptTokens: 100},
		{NCtx: 1000, PromptTokens: 900},
		{NCtx: 1000, PromptTokens: 400},
	}
	if got := contextPressure(slots); got != 0.9 {
		t.Errorf("contextPressure = %v, want 0.9", got)
	}
	if got := contextPressure(nil); got != 0 {
		t.Errorf("contextPressure(nil) = %v, want 0", got)
	}
}

// Efficiency is only meaningful while decoding; a stale rate against idle GPU
// power would otherwise report a rising, meaningless figure.
func TestEfficiencyRequiresActiveDecoding(t *testing.T) {
	base := Snapshot{
		GPUs:               []gpu.Device{{PowerWatts: 300, HasPower: true}},
		DecodeTokensPerSec: 60,
		HasDecodeMeasured:  true,
	}

	active := base
	active.DecodeStepsPerSec = 20
	active.DecodeAge = time.Second
	deriveEfficiency(&active)
	if !active.HasEfficiency {
		t.Fatal("HasEfficiency = false while decoding")
	}
	if got := active.TokensPerJoule; got != 0.2 {
		t.Errorf("TokensPerJoule = %v, want 0.2", got)
	}

	idle := base
	idle.DecodeStepsPerSec = 0
	idle.DecodeAge = time.Second
	deriveEfficiency(&idle)
	if idle.HasEfficiency {
		t.Error("HasEfficiency = true while idle")
	}

	stale := base
	stale.DecodeStepsPerSec = 20
	stale.DecodeAge = time.Minute
	deriveEfficiency(&stale)
	if stale.HasEfficiency {
		t.Error("HasEfficiency = true for a stale measurement")
	}
}

func TestHistoryRingEvictsOldest(t *testing.T) {
	h := NewHistory(3)
	for _, v := range []float64{1, 2, 3, 4, 5} {
		h.Push(v)
	}
	got := h.Values()
	if len(got) != 3 || got[0] != 3 || got[2] != 5 {
		t.Errorf("Values = %v, want [3 4 5]", got)
	}
	if h.Max() != 5 {
		t.Errorf("Max = %v, want 5", h.Max())
	}
	if h.Last() != 5 {
		t.Errorf("Last = %v, want 5", h.Last())
	}

	h.Reset()
	if len(h.Values()) != 0 || h.Max() != 0 || h.Last() != 0 {
		t.Error("Reset did not clear the ring")
	}
}
