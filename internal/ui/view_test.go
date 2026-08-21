package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pefman/ltop/internal/collect"
	"github.com/pefman/ltop/internal/config"
	"github.com/pefman/ltop/internal/format"
	"github.com/pefman/ltop/internal/gpu"
	"github.com/pefman/ltop/internal/host"
	"github.com/pefman/ltop/internal/llama"
	"github.com/pefman/ltop/internal/update"
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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

func TestStatsLineShowsLifetimeCounters(t *testing.T) {
	m := newTestModel(t, 120)
	snap := sampleSnapshot()
	snap.EnergyWh, snap.HasEnergy = 12.4, true
	m.snap = snap

	stats := lineWith(t, m.View(), "STATS")
	for _, want := range []string{"generated", "prefilled", "cached", "decodes", "saved"} {
		if !strings.Contains(stats, want) {
			t.Errorf("stats line missing %q:\n%s", want, stats)
		}
	}
	if strings.Contains(stats, "12.4Wh") || strings.Contains(stats, "€") {
		t.Errorf("cost figures still on stats line:\n%s", stats)
	}
}

func TestCostsLineShowsEnergyTariffAndCurrency(t *testing.T) {
	m := newTestModel(t, 120)
	snap := sampleSnapshot()
	snap.EnergyWh, snap.HasEnergy = 12.4, true
	m.snap = snap

	out := m.View()
	costs := lineWith(t, out, "COSTS")
	for _, want := range []string{"12.4Wh", "€0.0025", "€0.20/kWh", "EUR", "tok/J"} {
		if !strings.Contains(costs, want) {
			t.Errorf("costs line missing %q:\n%s", want, costs)
		}
	}
	if strings.Contains(lineWith(t, out, "QUEUE"), "tok/J") {
		t.Error("tok/J still on the queue line")
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
func TestStatsAndCostsHiddenWithoutMetrics(t *testing.T) {
	m := newTestModel(t, 110)
	snap := sampleSnapshot()
	snap.HasMetrics = false
	m.snap = snap

	out := m.View()
	if strings.Contains(out, "STATS") {
		t.Error("stats rendered without metrics")
	}
	if strings.Contains(out, "COSTS") {
		t.Error("costs rendered without metrics")
	}
}

// The totals row must say when it is counting from a reset rather than from
// server start, or a small number looks like an idle server.
func TestStatsLabelChangesAfterReset(t *testing.T) {
	m := newTestModel(t, 110)
	snap := sampleSnapshot()
	snap.StatsReset = true
	snap.StatsSince = 5 * time.Minute
	m.snap = snap

	out := m.View()
	if !strings.Contains(out, "SINCE") {
		t.Error("reset stats not labelled")
	}
	if !strings.Contains(lineWith(t, out, "SINCE"), "over 5m") {
		t.Errorf("reset window not shown on stats:\n%s", out)
	}
	if !strings.Contains(out, "COSTS") {
		t.Error("costs line missing after reset")
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

func TestGPURowShowsEuroCostRightOfWatts(t *testing.T) {
	m := newTestModel(t, 120)
	snap := sampleSnapshot()
	// 100 Wh at the default €0.20/kWh is €0.02.
	snap.HasEnergy = true
	snap.EnergyWh = 100
	snap.GPUEnergyWh = map[int]float64{0: 100}
	m.snap = snap

	out := m.View()
	gpuLine := gpuLineOf(t, out)
	wattsAt := strings.Index(gpuLine, "333/450W")
	if wattsAt < 0 {
		t.Fatalf("wattage missing from GPU line:\n%s", gpuLine)
	}
	euroAt := strings.Index(gpuLine[wattsAt:], "€0.0200")
	if euroAt < 0 {
		t.Fatalf("euro cost missing to the right of wattage:\n%s", gpuLine)
	}
}

func TestPriceKeysStepByTenCents(t *testing.T) {
	m := newTestModel(t, 120)
	m.paused = true
	if got := m.priceEUR(); got != 0.20 {
		t.Fatalf("default price = €%.2f/kWh, want €0.20", got)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if got := m.priceEUR(); got != 0.30 {
		t.Errorf("w = €%.2f/kWh, want €0.30", got)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	if got := m.priceEUR(); got != 0.20 {
		t.Errorf("W = €%.2f/kWh, want €0.20", got)
	}

	for i := 0; i < 30; i++ {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	}
	if got := m.priceEUR(); got != 0 {
		t.Errorf("price went below zero: €%.2f", got)
	}
}

func TestEuroCostScalesWithPrice(t *testing.T) {
	m := newTestModel(t, 120)
	m.paused = true
	snap := sampleSnapshot()
	snap.HasEnergy = true
	snap.EnergyWh = 100
	snap.GPUEnergyWh = map[int]float64{0: 100}
	m.snap = snap

	if !strings.Contains(m.View(), "€0.0200") {
		t.Fatalf("default cost missing:\n%s", gpuLineOf(t, m.View()))
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	out := m.View()
	if !strings.Contains(out, "€0.0300") {
		t.Errorf("cost did not follow €0.30/kWh:\n%s", gpuLineOf(t, out))
	}
	if !strings.Contains(out, "€0.30/kWh") {
		t.Errorf("footer tariff missing:\n%s", out)
	}
}

func TestFooterAndHelpShowPriceKeys(t *testing.T) {
	m := newTestModel(t, 140)
	out := m.View()
	if !strings.Contains(out, "w/W price") {
		t.Error("w/W price missing from footer")
	}
	if !strings.Contains(out, "c currency") {
		t.Error("c currency missing from footer")
	}
	if !strings.Contains(out, "€0.20/kWh") {
		t.Error("current tariff missing from costs line")
	}

	m.showHelp = true
	help := m.View()
	if !strings.Contains(help, "w / W") {
		t.Error("help missing w / W")
	}
	if !strings.Contains(help, "0.10") {
		t.Error("help missing the 0.10 step")
	}
}

func TestUpdateBannerAndInstallKey(t *testing.T) {
	m := newTestModel(t, 120)
	if strings.Contains(m.View(), "press u to install") {
		t.Error("update banner shown without an available update")
	}

	m.update = &update.Available{Version: "9.9.9"}
	out := m.View()
	if !strings.Contains(out, "v9.9.9") {
		t.Errorf("banner missing version:\n%s", out)
	}
	if !strings.Contains(out, "press u to install") {
		t.Error("banner missing install hint")
	}
	if !strings.Contains(out, "u update") {
		t.Error("u update missing from footer")
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if !m.updating {
		t.Error("u did not start the install")
	}
	if cmd == nil {
		t.Error("u did not return an install command")
	}

	m2 := newTestModel(t, 100)
	if _, cmd := m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")}); cmd != nil || m2.updating {
		t.Error("u without an update should be a no-op")
	}
}

func TestUpdateFailureShowsManualInstall(t *testing.T) {
	m := newTestModel(t, 160)
	m.updateErr = errTest{}
	out := m.View()
	if !strings.Contains(out, "update failed") {
		t.Error("missing failure text")
	}
	if !strings.Contains(out, "update manually") {
		t.Error("missing manual-install hint")
	}
	if !strings.Contains(out, "releases/latest/download/ltop_linux_") {
		t.Errorf("missing download URL:\n%s", out)
	}
	if !strings.Contains(out, "install -Dm755 ltop") {
		t.Error("missing install command")
	}
}

func TestCurrencyKeyCyclesIncludingSEK(t *testing.T) {
	m := newTestModel(t, 140)
	m.paused = true
	snap := sampleSnapshot()
	snap.HasEnergy = true
	snap.EnergyWh = 100
	snap.GPUEnergyWh = map[int]float64{0: 100}
	m.snap = snap

	if m.currency().Code != "EUR" {
		t.Fatalf("default currency = %s, want EUR", m.currency().Code)
	}

	seen := map[string]bool{}
	n := len(format.Currencies)
	for i := 0; i < n; i++ {
		seen[m.currency().Code] = true
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	}
	if m.currency().Code != "EUR" {
		t.Errorf("c did not wrap back to EUR, got %s", m.currency().Code)
	}
	for _, code := range []string{"EUR", "USD", "GBP", "SEK"} {
		if !seen[code] {
			t.Errorf("cycle missing %s", code)
		}
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if m.currency().Code != "AUD" {
		t.Errorf("C from EUR = %s, want AUD (previous in cycle)", m.currency().Code)
	}

	// Land on SEK and check the GPU cost and footer follow.
	for m.currency().Code != "SEK" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	}
	out := m.View()
	if !strings.Contains(gpuLineOf(t, out), "0.0200 SEK") {
		t.Errorf("SEK cost missing from GPU line:\n%s", gpuLineOf(t, out))
	}
	if !strings.Contains(out, "0.20 SEK/kWh") {
		t.Errorf("SEK tariff missing from footer:\n%s", out)
	}
	if !strings.Contains(lineWith(t, out, "COSTS"), "SEK") {
		t.Error("SEK missing from costs line")
	}

	m.showHelp = true
	help := m.View()
	if !strings.Contains(help, "c / C") {
		t.Error("help missing c / C")
	}
}

func TestCurrencyAndPricePersist(t *testing.T) {
	m := newTestModel(t, 100)
	m.paused = true
	for m.currency().Code != "SEK" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")}) // 0.40

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load after c/w: %v", err)
	}
	if saved.Currency != "SEK" {
		t.Errorf("saved currency = %q, want SEK", saved.Currency)
	}
	if saved.KWhPrice != 0.40 {
		t.Errorf("saved kWh price = %v, want 0.40", saved.KWhPrice)
	}

	again := newModel(t.Context(), saved)
	if again.currency().Code != "SEK" {
		t.Errorf("reloaded currency = %s, want SEK", again.currency().Code)
	}
	if again.priceEUR() != 0.40 {
		t.Errorf("reloaded price = %v, want 0.40", again.priceEUR())
	}
}

func TestUnknownSavedCurrencyFallsBackToEUR(t *testing.T) {
	cfg := testConfig()
	cfg.Currency = "XXX"
	m := newModel(t.Context(), cfg)
	if m.currency().Code != "EUR" {
		t.Errorf("currency = %s, want EUR", m.currency().Code)
	}
}

func gpuLineOf(t *testing.T, out string) string {
	t.Helper()
	return lineWith(t, out, "GPU0")
}

func lineWith(t *testing.T, out, label string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, label) {
			return line
		}
	}
	t.Fatalf("no %s line in:\n%s", label, out)
	return ""
}
