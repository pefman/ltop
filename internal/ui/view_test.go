package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pefman/ltop/internal/collect"
	"github.com/pefman/ltop/internal/gpu"
	"github.com/pefman/ltop/internal/host"
	"github.com/pefman/ltop/internal/llama"
)

// sampleSnapshot mirrors a real observation of a llama.cpp server running
// qwen3.8-27b with speculative decoding on an RTX 4090.
func sampleSnapshot() collect.Snapshot {
	return collect.Snapshot{
		At:         time.Now(),
		ScrapeR:    27 * time.Millisecond,
		Online:     true,
		HasMetrics: true,
		HasSlots:   true,
		Props: llama.Props{
			ModelPath: "/home/u/qwen3.8/Qwen3.8-27B-UD-Q4_K_XL.gguf",
			BuildInfo: "b10430-4c1a0af40",
		},
		Model: llama.Model{
			ID: "qwen3.8-27b",
			Meta: llama.ModelMeta{
				NCtx: 140032, NCtxTrain: 262144,
				NParams: 27320697856, Size: 17912397824, FType: "Q4_K - Small",
			},
		},
		Slots: []llama.Slot{
			{ID: 0, NCtx: 140032, Speculative: true, IsProcessing: true,
				TaskID: 7580, PromptTokens: 51716, PromptCached: 50750},
		},
		Raw: llama.Metrics{
			PromptTokensTotal: 220400, PromptCachedTotal: 2380000,
			PromptSecondsTotal: 189.5, PredictedTokensTotal: 26100,
			PredictedSecondsTotal: 598.2, DecodeTotal: 9225, TokensMax: 140031,
			SpecDraftTokensTotal: 21600, SpecAcceptedTokensTotal: 13629,
			SpecDraftsTotal: 7207, SpecAcceptedPerPos: []float64{5643, 4447, 3527},
			RequestsProcessing: 1, BusySlotsPerDecode: 1,
		},
		Host: host.Stats{
			CPUPercent: 43.8, CPUCores: 28,
			MemTotalBytes: 67_100_000_000, MemAvailableBytes: 31_900_000_000,
			LoadAvg1: 9.05,
		},
		GPUs: []gpu.Device{{
			Index: 0, Name: "NVIDIA GeForce RTX 4090", Vendor: "NVIDIA",
			UtilPercent: 68, HasUtil: true,
			MemUsedBytes: 25_131_000_000, MemTotalBytes: 25_757_000_000, HasMem: true,
			TempCelsius: 64, HasTemp: true,
			PowerWatts: 333, PowerLimitWatts: 450, HasPower: true,
		}},
		DecodeStepsPerSec:  21.27,
		DecodeTokensPerSec: 43.6, DecodeAge: 12 * time.Second, HasDecodeMeasured: true,
		PrefillTokensPerSec: 1163, PrefillAge: 64 * time.Second, HasPrefillMeasured: true,
		ContextPressure: 0.369,
		TokensPerJoule:  0.131, HasEfficiency: true,
	}
}

func newTestModel(t *testing.T, width int) *model {
	t.Helper()
	m := newModel(t.Context(), testConfig())
	m.width, m.height = width, 40
	m.snap = sampleSnapshot()
	m.polls = 2
	for _, v := range []float64{4, 9, 14, 19, 21, 18, 12, 7, 15, 21} {
		m.collector.StepHist.Push(v)
	}
	return m
}

func TestViewRendersKeyMetrics(t *testing.T) {
	m := newTestModel(t, 108)
	out := m.View()
	t.Logf("\n%s", out)

	want := []string{
		"Qwen3.8-27B-UD-Q4_K_XL",
		"b10430-4c1a0af40",
		"GPU0",
		"CPU",
		"RAM",
		"DECODE",
		"steps/s",
		"PREFILL",
		"KV CACHE",
		"CONTEXT",
		"SPEC",
		"SLOT",
		"running",
		"tok/J",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("view is missing %q", w)
		}
	}
}

// Every rendered line must fit the terminal or the layout will wrap and
// corrupt the display.
func TestViewFitsTerminalWidth(t *testing.T) {
	for _, width := range []int{72, 80, 100, 120, 200} {
		m := newTestModel(t, width)
		for i, line := range strings.Split(m.View(), "\n") {
			if got := visibleWidth(line); got > width {
				t.Errorf("width %d: line %d is %d cells: %q", width, i, got, line)
			}
		}
	}
}

func TestOfflineViewDoesNotPanic(t *testing.T) {
	m := newTestModel(t, 100)
	m.snap = collect.Snapshot{Online: false, Err: errTest{}}
	out := m.View()
	if !strings.Contains(out, "unreachable") {
		t.Errorf("offline view missing error text:\n%s", out)
	}
}

// A server without --metrics must still render its panels, not an error page.
func TestViewWithoutMetricsStillRendersServer(t *testing.T) {
	m := newTestModel(t, 100)
	snap := sampleSnapshot()
	snap.HasMetrics = false
	m.snap = snap

	out := m.View()
	if strings.Contains(out, "offline") {
		t.Error("a server without --metrics rendered as offline")
	}
	for _, want := range []string{"online", "--metrics", "GPU0", "SLOT", "Qwen3.8-27B"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if strings.Contains(out, "KV CACHE") {
		t.Error("throughput panels rendered without metrics data")
	}
}

func TestViewShowsLoadingAndUnmatchedModel(t *testing.T) {
	m := newTestModel(t, 110)
	snap := sampleSnapshot()
	snap.Loading = true
	snap.ModelUnmatched = true
	m.snap = snap

	out := m.View()
	if !strings.Contains(out, "loading model") {
		t.Error("loading state not shown")
	}
	if !strings.Contains(out, "several models") {
		t.Error("unmatched model warning not shown")
	}
}

func TestOfflineViewHintsAtAuth(t *testing.T) {
	m := newTestModel(t, 100)
	m.snap = collect.Snapshot{Online: false, NeedsAuth: true, Err: errTest{}}
	if out := m.View(); !strings.Contains(out, "API key") {
		t.Errorf("auth hint missing:\n%s", out)
	}
}

func TestEmptySnapshotDoesNotPanic(t *testing.T) {
	m := newModel(t.Context(), testConfig())
	m.width, m.height = 100, 40
	if out := m.View(); out == "" {
		t.Error("empty view")
	}
}

func TestHelpView(t *testing.T) {
	m := newTestModel(t, 100)
	m.showHelp = true
	out := m.View()
	for _, w := range []string{"KEYS", "METRICS", "KV CACHE", "SPEC"} {
		if !strings.Contains(out, w) {
			t.Errorf("help missing %q", w)
		}
	}
}

type errTest struct{}

func (errTest) Error() string { return "dial tcp 127.0.0.1:11436: connection refused (unreachable)" }

func TestTotalsLineShowsLifetimeCounters(t *testing.T) {
	m := newTestModel(t, 110)
	snap := sampleSnapshot()
	snap.EnergyWh, snap.HasEnergy = 12.4, true
	m.snap = snap

	out := m.View()
	for _, want := range []string{"TOTALS", "generated", "prefilled", "cached", "decodes", "saved", "12.4Wh"} {
		if !strings.Contains(out, want) {
			t.Errorf("totals line missing %q", want)
		}
	}
}

// Context sizes are configuration values, so 262144 must not render as 262.1k.
func TestHeaderShowsRawContextSizes(t *testing.T) {
	m := newTestModel(t, 110)
	out := m.View()
	if !strings.Contains(out, "ctx 140032/262144") {
		t.Errorf("raw context sizes missing from header:\n%s", out)
	}
	if strings.Contains(out, "262.1k") {
		t.Error("context still rendered with SI rounding")
	}
}

// Without metrics there are no counters to total.
func TestTotalsHiddenWithoutMetrics(t *testing.T) {
	m := newTestModel(t, 110)
	snap := sampleSnapshot()
	snap.HasMetrics = false
	m.snap = snap

	if strings.Contains(m.View(), "TOTALS") {
		t.Error("totals rendered without metrics")
	}
}

// The totals row must say when it is counting from a reset rather than from
// server start, or a small number looks like an idle server.
func TestTotalsLabelChangesAfterReset(t *testing.T) {
	m := newTestModel(t, 110)
	snap := sampleSnapshot()
	snap.StatsReset = true
	snap.StatsSince = 5 * time.Minute
	m.snap = snap

	out := m.View()
	if !strings.Contains(out, "SINCE") {
		t.Error("reset totals not labelled")
	}
	if !strings.Contains(out, "over 5m") {
		t.Errorf("reset window not shown:\n%s", out)
	}
	if !strings.Contains(out, "z reset") {
		t.Error("z key missing from footer")
	}
}

func TestResetKeyInvokesCollector(t *testing.T) {
	m := newTestModel(t, 100)
	m.paused = true // keeps the handler from firing a poll

	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}); cmd != nil {
		t.Error("reset issued a poll while paused")
	}
}
