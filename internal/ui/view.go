package ui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/pefman/ltop/internal/buildinfo"
	"github.com/pefman/ltop/internal/format"
	"github.com/pefman/ltop/internal/gpu"
	"github.com/pefman/ltop/internal/update"
)

// labelCol is the width of the left-hand label column.
const labelCol = 9

func (m *model) View() string {
	if m.showHelp {
		return m.clamp(m.helpView())
	}

	p := m.palette
	var b strings.Builder

	b.WriteString(m.headerView())
	b.WriteString("\n")
	b.WriteString(m.updateBanner())

	if !m.snap.Online {
		b.WriteString(m.offlineView())
		b.WriteString(m.footerView())
		return m.clamp(b.String())
	}

	if m.snap.Restarted {
		b.WriteString(p.Warn.Render("  server restarted; counters rebaselined") + "\n")
	}
	if m.snap.ModelChanged {
		b.WriteString(p.Warn.Render("  model changed; history cleared") + "\n")
	}
	if m.snap.ModelUnmatched {
		b.WriteString(p.Warn.Render("  several models served here; metadata withheld to avoid mismatching") + "\n")
	}
	if !m.snap.HasMetrics {
		b.WriteString(p.Warn.Render("  no throughput data; restart llama-server with --metrics") + "\n")
	}

	b.WriteString(m.resourceView())
	if m.snap.HasMetrics {
		b.WriteString("\n")
		b.WriteString(m.throughputView())
		b.WriteString("\n")
		b.WriteString(m.qualityView())

		if m.showSpec && m.snap.Raw.SpecEnabled() {
			b.WriteString("\n")
			b.WriteString(m.specView())
		}
	}

	b.WriteString("\n")
	b.WriteString(m.slotsView())
	if m.snap.HasMetrics {
		b.WriteString(m.statsView())
		b.WriteString(m.costsView())
	}
	b.WriteString(m.footerView())
	return m.clamp(b.String())
}

// clamp truncates every line to the terminal width. Panels size themselves to
// the window, but label text is variable, so this is the final guard against a
// wrapped line corrupting the layout.
func (m *model) clamp(s string) string {
	return lipgloss.NewStyle().MaxWidth(m.width).Render(s)
}

func (m *model) headerView() string {
	p := m.palette
	s := m.snap

	name := s.Props.ModelName()
	if name == "" {
		name = "unknown model"
	}
	title := " ltop " + buildinfo.Version + "  " + name
	if meta := s.Model.Meta; meta.NParams > 0 {
		title += fmt.Sprintf("  %s  %s  %s",
			meta.FType, format.Count(float64(meta.NParams)), format.Bytes(uint64(meta.Size)))
	}

	status := " ● online "
	switch {
	case !s.Online:
		status = " ● offline "
	case s.Loading:
		status = " ● loading model "
	case s.Props.IsSleeping:
		status = " ● sleeping "
	case m.paused:
		status = " ‖ paused "
	}

	gap := m.width - lipgloss.Width(title) - lipgloss.Width(status)
	if gap < 1 {
		gap = 1
		title = format.Truncate(title, m.width-lipgloss.Width(status)-1)
	}
	head := p.Head.Render(title + strings.Repeat(" ", gap) + status)

	// Context sizes are configuration values, so they are shown exactly as
	// passed to -c rather than rounded, where 262144 would read as 262.1k.
	sub := fmt.Sprintf("  %s   ctx %d/%d   llama.cpp %s   up %s   scrape %s",
		m.collector.Endpoint(),
		s.Model.Meta.NCtx,
		s.Model.Meta.NCtxTrain,
		orDash(s.Props.BuildInfo),
		format.Duration(m.collector.Uptime()),
		s.ScrapeR.Round(time.Millisecond),
	)
	return head + "\n" + p.Muted.Render(format.Truncate(sub, m.width)) + "\n"
}

func (m *model) updateBanner() string {
	if m.update == nil && m.updateErr == nil && !m.updating {
		return ""
	}
	p := m.palette
	switch {
	case m.updating && m.update != nil:
		return p.Warn.Render(format.Truncate("  installing v"+m.update.Version+"…", m.width)) + "\n"
	case m.updateErr != nil:
		fail := p.Err.Render(format.Truncate("  update failed: "+m.updateErr.Error(), m.width))
		hint := p.Muted.Render(format.Truncate("  update manually:  "+update.ManualInstall(runtime.GOOS, runtime.GOARCH), m.width))
		return fail + "\n" + hint + "\n"
	case m.update != nil:
		msg := "  update v" + m.update.Version + " ready   press u to install"
		style := p.Warn
		if m.updateBlink {
			style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("214"))
		}
		return style.Render(pad(format.Truncate(msg, m.width), m.width)) + "\n"
	}
	return ""
}

func (m *model) offlineView() string {
	p := m.palette
	msg := "server unreachable"
	if m.snap.Err != nil {
		msg = m.snap.Err.Error()
	}
	hint := "retrying every " + m.interval().String()
	if m.snap.NeedsAuth {
		hint = "run 'ltop -reconfigure' to supply an API key"
	}
	return "\n" + p.Err.Render("  "+format.Truncate(msg, m.width-4)+"  ") +
		"\n\n" + p.Muted.Render("  "+hint) + "\n"
}

func (m *model) resourceView() string {
	p := m.palette
	s := m.snap
	w := m.barWidth()

	var b strings.Builder
	if m.showGPU {
		for _, d := range s.GPUs {
			wh := 0.0
			if s.GPUEnergyWh != nil {
				wh = s.GPUEnergyWh[d.Index]
			}
			cost := format.EnergyCostEUR(wh, m.priceEUR())
			b.WriteString(gpuRow(p, d, w, m.width, cost, m.currency()))
		}
	}

	cpuLabel := fmt.Sprintf("%5.1f%%  %d cores  load %.2f", s.Host.CPUPercent, s.Host.CPUCores, s.Host.LoadAvg1)
	b.WriteString("  " + row(p, "CPU", labelCol, gauge(p, s.Host.CPUPercent/100, w, cpuLabel)) + "\n")

	memLabel := fmt.Sprintf("%5.1f%%  %s of %s",
		s.Host.MemPercent()*100, format.Bytes(s.Host.MemUsedBytes()), format.Bytes(s.Host.MemTotalBytes))
	b.WriteString("  " + row(p, "RAM", labelCol, gauge(p, s.Host.MemPercent(), w, memLabel)) + "\n")
	return b.String()
}

func gpuRow(p Palette, d gpu.Device, w, total int, cost float64, cur format.Currency) string {
	label := fmt.Sprintf("%5.0f%%", d.UtilPercent)
	if d.HasMem {
		label += fmt.Sprintf("  %s of %s", format.Bytes(d.MemUsedBytes), format.Bytes(d.MemTotalBytes))
	}
	if d.HasTemp {
		label += fmt.Sprintf("  %.0f°C", d.TempCelsius)
	}
	if d.HasPower {
		label += fmt.Sprintf("  %.0f/%.0fW  %s", d.PowerWatts, d.PowerLimitWatts, cur.Format(cost))
	}

	name := fmt.Sprintf("GPU%d", d.Index)
	line := "  " + row(p, name, labelCol, gauge(p, d.UtilPercent/100, w, label)) + "\n"

	if d.HasMem {
		vramLabel := fmt.Sprintf("%5.1f%%  %s", d.MemPercent()*100, format.Truncate(d.Name, 28))
		line += "  " + row(p, "  vram", labelCol, gauge(p, d.MemPercent(), w, vramLabel)) + "\n"
	}
	return line
}

func (m *model) throughputView() string {
	p := m.palette
	s := m.snap
	sparkW := m.sparkWidth()

	var b strings.Builder

	liveStyle := p.Accent
	if s.DecodeStepsPerSec <= 0 {
		liveStyle = p.Muted
	}
	live := fmt.Sprintf("%7.1f steps/s", s.DecodeStepsPerSec)
	b.WriteString("  " + row(p, "DECODE", labelCol,
		liveStyle.Render(live)+"  "+sparkline(p, m.collector.StepHist.Values(), sparkW)) + "\n")

	b.WriteString("  " + row(p, "  tok/s", labelCol,
		measuredLabel(p, s.DecodeTokensPerSec, s.DecodeAge, s.HasDecodeMeasured)+
			p.Muted.Render(fmt.Sprintf("   lifetime %s", format.Rate(s.Raw.LifetimePredictedTokensPerSec())))) + "\n")

	b.WriteString("  " + row(p, "PREFILL", labelCol,
		measuredLabel(p, s.PrefillTokensPerSec, s.PrefillAge, s.HasPrefillMeasured)+
			p.Muted.Render(fmt.Sprintf("   lifetime %s", format.Rate(s.Raw.LifetimePromptTokensPerSec())))) + "\n")

	queue := fmt.Sprintf("%.0f processing   %.0f deferred   %.2f slots/decode",
		s.Raw.RequestsProcessing, s.Raw.RequestsDeferred, s.Raw.BusySlotsPerDecode)
	b.WriteString("  " + row(p, "QUEUE", labelCol, p.Value.Render(queue)) + "\n")
	return b.String()
}

// measuredLabel renders a throughput figure that llama.cpp only refreshes on
// request completion, dimming it as it ages so a stale number is never mistaken
// for a live one.
func measuredLabel(p Palette, rate float64, age time.Duration, ok bool) string {
	if !ok {
		return p.Muted.Render("      -- tok/s  awaiting a completed request")
	}
	style := p.Value
	if age > 30*time.Second {
		style = p.Muted
	}
	return style.Render(fmt.Sprintf("%7s tok/s", format.Rate(rate))) +
		p.Muted.Render(fmt.Sprintf("  measured %s ago", format.Duration(age)))
}

func (m *model) qualityView() string {
	p := m.palette
	s := m.snap
	w := m.barWidth()

	var b strings.Builder

	hit := s.Raw.CacheHitRate()
	hitLabel := fmt.Sprintf("%5.1f%%  %s reused / %s prefilled",
		hit*100, format.Count(s.Raw.PromptCachedTotal), format.Count(s.Raw.PromptTokensTotal))
	b.WriteString("  " + row(p, "KV CACHE", labelCol, qualityGauge(p, hit, w, hitLabel)) + "\n")

	ctxLabel := fmt.Sprintf("%5.1f%%  longest seen %s tok",
		s.ContextPressure*100, format.Count(s.Raw.TokensMax))
	b.WriteString("  " + row(p, "CONTEXT", labelCol, gauge(p, s.ContextPressure, w, ctxLabel)) + "\n")
	return b.String()
}

func (m *model) specView() string {
	p := m.palette
	mt := m.snap.Raw
	w := m.barWidth()

	rate := mt.SpecAcceptanceRate()
	label := fmt.Sprintf("%5.1f%%  %.2fx est speedup  %.2f accepted/draft",
		rate*100, mt.SpecSpeedup(), mt.SpecTokensPerDraft())

	var b strings.Builder
	b.WriteString("  " + row(p, "SPEC", labelCol, qualityGauge(p, rate, w, label)) + "\n")

	// Per-position acceptance shows where the draft stops paying for itself,
	// which is the signal for tuning draft length.
	if len(mt.SpecAcceptedPerPos) > 0 && mt.SpecDraftsTotal > 0 {
		var parts []string
		for i, v := range mt.SpecAcceptedPerPos {
			share := v / mt.SpecDraftsTotal
			parts = append(parts, p.quality(share).Render(fmt.Sprintf("pos%d %.0f%%", i, share*100)))
		}
		b.WriteString("  " + row(p, "  draft", labelCol, strings.Join(parts, p.Muted.Render("  ·  "))) + "\n")
	}
	return b.String()
}

func (m *model) slotsView() string {
	p := m.palette
	s := m.snap

	if len(s.Slots) == 0 {
		return "  " + p.Muted.Render("no slot detail; start llama-server with --slots") + "\n"
	}

	barW := 14
	header := fmt.Sprintf("  %-4s %-8s %-9s %10s %10s %7s  %s",
		"SLOT", "STATE", "TASK", "PROMPT", "CACHED", "CTX", "")
	var b strings.Builder
	b.WriteString(p.Label.Render(pad(format.Truncate(header, m.width), m.width)) + "\n")

	for _, sl := range s.Slots {
		state, style := "idle", p.Muted
		if sl.IsProcessing {
			state, style = "running", p.Good
		}
		used := sl.ContextUsed()

		line := fmt.Sprintf("  %-4d %s %-9d %10d %10d %6.1f%%  %s",
			sl.ID,
			style.Render(pad(state, 8)),
			sl.TaskID,
			sl.PromptTokens,
			sl.PromptCached,
			used*100,
			gauge(p, used, barW, ""),
		)
		b.WriteString(line + "\n")
	}
	return b.String()
}

// statsView renders lifetime counters: what the server has processed since it
// started, as opposed to the rates above it.
func (m *model) statsView() string {
	p := m.palette
	mt := m.snap.Raw

	label := "STATS"
	if m.snap.StatsReset {
		label = "SINCE z"
	}

	parts := []string{
		format.Count(mt.PredictedTokensTotal) + " generated",
		format.Count(mt.PromptTokensTotal) + " prefilled",
		format.Count(mt.PromptCachedTotal) + " cached",
		format.Count(mt.DecodeTotal) + " decodes",
	}
	if saved := mt.PrefillTimeSaved(); saved > 0 {
		parts = append(parts, "~"+format.Compact(saved)+" saved")
	}
	if m.snap.StatsReset {
		parts = append(parts, "over "+format.Compact(m.snap.StatsSince))
	}

	return "\n  " + row(p, label, labelCol,
		p.Value.Render(strings.Join(parts, p.Muted.Render("  ")))) + "\n"
}

// costsView renders session energy, the running electricity bill, and the
// tariff the w/W and c/C keys adjust.
func (m *model) costsView() string {
	p := m.palette
	cost := format.EnergyCostEUR(m.snap.EnergyWh, m.priceEUR())
	cur := m.currency()

	parts := make([]string, 0, 5)
	if m.snap.HasEnergy {
		parts = append(parts, fmt.Sprintf("%.1fWh", m.snap.EnergyWh))
	}
	parts = append(parts, cur.Format(cost), cur.Tariff(m.priceEUR()), cur.Code)
	if m.snap.HasEfficiency {
		parts = append(parts, fmt.Sprintf("%.3f tok/J", m.snap.TokensPerJoule))
	}

	return "  " + row(p, "COSTS", labelCol,
		p.Value.Render(strings.Join(parts, p.Muted.Render("  ")))) + "\n"
}

func (m *model) footerView() string {
	p := m.palette
	keys := []string{
		"q quit",
		"p pause",
		"+/- " + m.interval().String(),
		"s spec",
		"g gpu",
		"r refresh",
		"z reset",
		"w/W price",
		"c currency",
		"? help",
	}
	if m.update != nil {
		keys = append(keys[:len(keys)-1], "u update", "? help")
	}
	return "\n" + p.KeyHint.Render(pad("  "+strings.Join(keys, "   "), m.width))
}

func (m *model) helpView() string {
	p := m.palette
	lines := []string{
		p.Title.Render("ltop — activity monitor for llama.cpp"),
		"",
		p.Label.Render("KEYS"),
		"  q / esc      quit",
		"  p / space    pause polling",
		"  + / -        faster / slower poll interval",
		"  s            toggle the speculative decoding panel",
		"  g            toggle the GPU panel",
		"  r            force one refresh",
		"  u            install a pending self-update (when advertised)",
		"  z            reset the stats window to now",
		"  w / W        raise / lower electricity price by 0.10/kWh",
		"  c / C        cycle electricity currency (EUR, USD, GBP, SEK, …)",
		"  ?            close this help",
		"",
		p.Label.Render("METRICS"),
		"  DECODE steps/s   llama_decode() calls per second. This is the only",
		"                   counter llama.cpp advances while a request is still",
		"                   generating, so it is the live activity signal.",
		"  tok/s measured   exact throughput from token and time counters.",
		"                   llama.cpp only publishes these when a request",
		"                   completes, so the value is held and aged.",
		"  KV CACHE         share of prompt tokens reused instead of re-prefilled.",
		"                   Low values mean prompt caching is not being hit.",
		"  CONTEXT          fullest slot's context occupancy.",
		"  SPEC             draft-token acceptance. Below roughly 35% the draft",
		"                   model usually costs more than it saves. The per-",
		"                   position row shows where drafting stops paying off,",
		"                   which is how to size the draft length.",
		"  tok/J            decode tokens per joule of GPU energy.",
		"  STATS            lifetime counters from the server, plus an estimate of",
		"                   the prefill wall time the KV cache avoided.",
		"                   Press z to count from now instead; llama.cpp cannot",
		"                   zero its own counters, so ltop records a baseline and",
		"                   reports the difference. Cache and spec rates follow.",
		"  COSTS            GPU energy observed this session, the running bill at",
		"                   the current /kWh tariff, and tok/J while decoding.",
		"                   w/W steps the tariff by 0.10; c/C cycles currency.",
		"                   Each GPU also shows its own running cost next to watts.",
		"",
		p.Muted.Render("press ? to return"),
	}
	return strings.Join(lines, "\n") + "\n"
}

// barWidth scales the meters to the window, shrinking first so the numeric
// labels beside them stay readable on narrow terminals.
func (m *model) barWidth() int {
	w := m.width / 4
	if w < 10 {
		w = 10
	}
	if w > 30 {
		w = 30
	}
	return w
}

func (m *model) sparkWidth() int {
	w := m.width - labelCol - 26
	if w < 8 {
		w = 8
	}
	if w > 80 {
		w = 80
	}
	return w
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
