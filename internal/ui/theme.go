package ui

import "github.com/charmbracelet/lipgloss"

// Palette holds every style the dashboard draws with.
type Palette struct {
	Title   lipgloss.Style
	Label   lipgloss.Style
	Value   lipgloss.Style
	Muted   lipgloss.Style
	Head    lipgloss.Style
	Good    lipgloss.Style
	Warn    lipgloss.Style
	Crit    lipgloss.Style
	Accent  lipgloss.Style
	Err     lipgloss.Style
	KeyHint lipgloss.Style
}

func newPalette() Palette {
	return Palette{
		Title:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")),
		Label:   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		Value:   lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		Muted:   lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		Head:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("39")),
		Good:    lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		Warn:    lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		Crit:    lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		Accent:  lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		Err:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("124")),
		KeyHint: lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("245")),
	}
}

// level picks a style from a 0..1 ratio, where higher means more loaded.
func (p Palette) level(ratio float64) lipgloss.Style {
	switch {
	case ratio >= 0.90:
		return p.Crit
	case ratio >= 0.70:
		return p.Warn
	default:
		return p.Good
	}
}

// quality picks a style from a 0..1 ratio where higher is better, which is the
// inverse of level and suits cache hit and acceptance rates.
func (p Palette) quality(ratio float64) lipgloss.Style {
	switch {
	case ratio >= 0.60:
		return p.Good
	case ratio >= 0.35:
		return p.Warn
	default:
		return p.Crit
	}
}
