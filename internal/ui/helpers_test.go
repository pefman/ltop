package ui

import (
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/pefman/ltop/internal/config"
)

// visibleWidth is the rendered cell width of a line, ignoring ANSI styling.
func visibleWidth(s string) int { return lipgloss.Width(s) }

func testConfig() config.Config {
	return config.Config{
		Endpoint:       "http://127.0.0.1:11436",
		PollIntervalMS: int(time.Second / time.Millisecond),
	}
}
