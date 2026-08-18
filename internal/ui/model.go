// Package ui renders the ltop dashboard as a Bubble Tea program.
package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pefman/ltop/internal/collect"
	"github.com/pefman/ltop/internal/config"
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

	// polls counts completed scrapes, used to distinguish "starting up" from
	// "no data available".
	polls int
}

func newModel(ctx context.Context, cfg config.Config) *model {
	m := &model{
		ctx:       ctx,
		collector: collect.New(cfg.Endpoint, cfg.APIKey),
		palette:   newPalette(),
		width:     100,
		height:    30,
		started:   time.Now(),
		showSpec:  true,
		showGPU:   true,
	}
	m.intervalIdx = nearestInterval(cfg.PollInterval())
	return m
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

func (m *model) Init() tea.Cmd { return m.poll() }

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
		if m.paused {
			return m, m.scheduleNext()
		}
		return m, m.poll()
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
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
	}
	return m, nil
}

// Run starts the interactive dashboard.
func Run(ctx context.Context, cfg config.Config) error {
	m := newModel(ctx, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	if err != nil && ctx.Err() != nil {
		return nil
	}
	return err
}
