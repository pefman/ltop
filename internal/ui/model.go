// Package ui renders the ltop dashboard as a Bubble Tea program.
package ui

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pefman/ltop/internal/buildinfo"
	"github.com/pefman/ltop/internal/collect"
	"github.com/pefman/ltop/internal/config"
	"github.com/pefman/ltop/internal/format"
	"github.com/pefman/ltop/internal/update"
)

// Electricity price is stored in tenths of a euro per kWh so w/W steps
// stay on exact 0.1 increments without float drift.
const (
	defaultPriceTenths = int(format.DefaultEURPerKWh * 10)
	maxPriceTenths     = 99
)

// intervals are the poll cadences the +/- keys cycle through.
var intervals = []time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	5 * time.Second,
}

type snapshotMsg collect.Snapshot

type model struct {
	ctx       context.Context
	cfg       config.Config
	collector *collect.Collector
	palette   Palette

	snap    collect.Snapshot
	width   int
	height  int
	started time.Time

	intervalIdx int
	paused      bool
	showSpec    bool
	showGPU     bool
	showHelp    bool
	priceTenths int
	currencyIdx int

	update      *update.Available
	updateErr   error
	updateBlink bool
	updating    bool
	restartTo   string

	// polls counts completed scrapes, used to distinguish "starting up" from
	// "no data available".
	polls int
}

func newModel(ctx context.Context, cfg config.Config) *model {
	m := &model{
		ctx:         ctx,
		cfg:         cfg,
		collector:   collect.New(cfg.Endpoint, cfg.APIKey),
		palette:     newPalette(),
		width:       100,
		height:      30,
		started:     time.Now(),
		showSpec:    true,
		showGPU:     true,
		priceTenths: priceTenthsFrom(cfg.KWhPrice),
		currencyIdx: format.CurrencyIndex(cfg.Currency),
	}
	m.intervalIdx = nearestInterval(cfg.PollInterval())
	return m
}

func priceTenthsFrom(perKWh float64) int {
	if perKWh <= 0 {
		return defaultPriceTenths
	}
	n := int(math.Round(perKWh * 10))
	if n < 0 {
		return 0
	}
	if n > maxPriceTenths {
		return maxPriceTenths
	}
	return n
}

func nearestInterval(d time.Duration) int {
	best, bestDiff := 2, time.Duration(1<<62)
	for i, v := range intervals {
		diff := v - d
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			best, bestDiff = i, diff
		}
	}
	return best
}

func (m *model) interval() time.Duration { return intervals[m.intervalIdx] }

func (m *model) priceEUR() float64 { return float64(m.priceTenths) / 10 }

func (m *model) currency() format.Currency {
	if m.currencyIdx < 0 || m.currencyIdx >= len(format.Currencies) {
		return format.EUR
	}
	return format.Currencies[m.currencyIdx]
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.poll(), m.checkUpdate()) }

func (m *model) checkUpdate() tea.Cmd {
	return func() tea.Msg {
		av, err := update.Check(m.ctx, buildinfo.Version, runtime.GOOS, runtime.GOARCH)
		if err != nil || av == nil {
			return updateMsg{}
		}
		return updateMsg{avail: av}
	}
}

type updateMsg struct {
	avail *update.Available
}

type updateDoneMsg struct {
	err error
	exe string
}

// poll runs one scrape off the UI goroutine.
func (m *model) poll() tea.Cmd {
	return func() tea.Msg {
		return snapshotMsg(m.collector.Poll(m.ctx))
	}
}

func (m *model) scheduleNext() tea.Cmd {
	return tea.Tick(m.interval(), func(time.Time) tea.Msg { return tickMsg{} })
}

type tickMsg struct{}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case snapshotMsg:
		m.snap = collect.Snapshot(msg)
		m.polls++
		return m, m.scheduleNext()

	case tickMsg:
		if m.update != nil && !m.updating {
			m.updateBlink = !m.updateBlink
		}
		if m.paused {
			return m, m.scheduleNext()
		}
		return m, m.poll()

	case updateMsg:
		m.update = msg.avail
		return m, nil

	case updateDoneMsg:
		m.updating = false
		if msg.err != nil {
			m.updateErr = msg.err
			return m, nil
		}
		if msg.exe == "" {
			m.updateErr = fmt.Errorf("update installed but restart path is empty")
			return m, nil
		}
		m.restartTo = msg.exe
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.updating {
		return m, nil
	}
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "u", "U":
		if m.update == nil {
			return m, nil
		}
		m.updating = true
		m.updateErr = nil
		return m, m.installUpdate()
	case "p", " ":
		m.paused = !m.paused
	case "+", "=":
		if m.intervalIdx > 0 {
			m.intervalIdx--
		}
	case "-", "_":
		if m.intervalIdx < len(intervals)-1 {
			m.intervalIdx++
		}
	case "s":
		m.showSpec = !m.showSpec
	case "g":
		m.showGPU = !m.showGPU
	case "?", "h":
		m.showHelp = !m.showHelp
	case "r":
		if !m.paused {
			return m, m.poll()
		}
	case "z":
		m.collector.ResetStats()
		if !m.paused {
			return m, m.poll()
		}
	case "w":
		if m.priceTenths < maxPriceTenths {
			m.priceTenths++
			m.persistCosts()
		}
	case "W":
		if m.priceTenths > 0 {
			m.priceTenths--
			m.persistCosts()
		}
	case "c":
		m.currencyIdx = (m.currencyIdx + 1) % len(format.Currencies)
		m.persistCosts()
	case "C":
		m.currencyIdx = (m.currencyIdx + len(format.Currencies) - 1) % len(format.Currencies)
		m.persistCosts()
	}
	return m, nil
}

func (m *model) installUpdate() tea.Cmd {
	av := m.update
	return func() tea.Msg {
		c := &update.Client{
			GOOS:      runtime.GOOS,
			GOARCH:    runtime.GOARCH,
			UserAgent: "ltop/" + buildinfo.Version,
			NoExec:    true,
		}
		path, err := c.Apply(context.Background(), av)
		return updateDoneMsg{err: err, exe: path}
	}
}

func (m *model) persistCosts() {
	m.cfg.Currency = m.currency().Code
	m.cfg.KWhPrice = m.priceEUR()
	_ = config.Save(m.cfg)
}

// Run starts the interactive dashboard.
func Run(ctx context.Context, cfg config.Config) error {
	m := newModel(ctx, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	final, err := p.Run()
	if err != nil && ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return err
	}
	done, ok := final.(*model)
	if !ok || done.restartTo == "" {
		return nil
	}
	err = syscall.Exec(done.restartTo, os.Args, os.Environ())
	return fmt.Errorf("updated to v%s; restart failed: %w", done.update.Version, err)
}
