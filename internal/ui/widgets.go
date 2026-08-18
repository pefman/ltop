package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// sparkChars are ordered from lowest to highest magnitude.
var sparkChars = []rune("▁▂▃▄▅▆▇█")

// gauge renders a bracketed meter coloured by load, with a trailing label.
func gauge(p Palette, ratio float64, width int, label string) string {
	if width < 3 {
		return label
	}
	inner := width - 2
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * float64(inner)))

	bar := p.level(ratio).Render(strings.Repeat("|", filled)) +
		p.Muted.Render(strings.Repeat(" ", inner-filled))
	out := p.Muted.Render("[") + bar + p.Muted.Render("]")
	if label != "" {
		out += " " + p.Value.Render(label)
	}
	return out
}

// qualityGauge renders a meter where a higher ratio is better.
func qualityGauge(p Palette, ratio float64, width int, label string) string {
	if width < 3 {
		return label
	}
	inner := width - 2
	ratio = clamp01(ratio)
	filled := int(math.Round(ratio * float64(inner)))

	bar := p.quality(ratio).Render(strings.Repeat("|", filled)) +
		p.Muted.Render(strings.Repeat(" ", inner-filled))
	out := p.Muted.Render("[") + bar + p.Muted.Render("]")
	if label != "" {
		out += " " + p.Value.Render(label)
	}
	return out
}

// sparkline renders the most recent width values, scaled to the window's own
// peak so idle periods still show shape.
func sparkline(p Palette, values []float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(values) == 0 {
		return p.Muted.Render(strings.Repeat("·", width))
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}

	peak := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
	}

	var b strings.Builder
	if pad := width - len(values); pad > 0 {
		b.WriteString(p.Muted.Render(strings.Repeat("·", pad)))
	}
	for _, v := range values {
		if peak <= 0 {
			b.WriteRune(sparkChars[0])
			continue
		}
		idx := int(math.Round(clamp01(v/peak) * float64(len(sparkChars)-1)))
		b.WriteRune(sparkChars[idx])
	}
	return p.Accent.Render(b.String())
}

// row lays a label and value out on one line with a fixed label column.
func row(p Palette, label string, labelWidth int, value string) string {
	return p.Label.Render(pad(label, labelWidth)) + " " + value
}

func pad(s string, width int) string {
	if n := width - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padLeft(s string, width int) string {
	if n := width - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

func clamp01(v float64) float64 {
	switch {
	case v < 0 || math.IsNaN(v):
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
