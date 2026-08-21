// Package format renders numbers and meters for terminal display.
package format

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// DefaultEURPerKWh is a round European-ish household tariff used until the
// user nudges it with w/W.
const DefaultEURPerKWh = 0.20

// Bytes renders a byte count with binary units.
func Bytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTP"[exp])
}

// Count renders a large tally with SI-style suffixes.
func Count(v float64) string {
	switch a := math.Abs(v); {
	case a >= 1e12:
		return fmt.Sprintf("%.2fT", v/1e12)
	case a >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case a >= 1e6:
		return fmt.Sprintf("%.2fM", v/1e6)
	case a >= 1e4:
		return fmt.Sprintf("%.1fk", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

// Percent renders a 0..1 ratio.
func Percent(ratio float64) string { return fmt.Sprintf("%.1f%%", ratio*100) }

// Euros renders an electricity cost in euros.
func Euros(v float64) string { return EUR.Format(v) }

// EnergyCostEUR converts watt-hours at a euro-per-kWh tariff into euros.
func EnergyCostEUR(wattHours, eurPerKWh float64) float64 {
	if wattHours <= 0 || eurPerKWh <= 0 {
		return 0
	}
	return wattHours / 1000 * eurPerKWh
}

// Rate renders a tokens-per-second figure.
func Rate(v float64) string {
	if v <= 0 {
		return "  --  "
	}
	if v >= 1000 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

// Duration renders an uptime-style duration.
func Duration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// Compact renders a duration at two significant units, for figures that are
// scanned rather than read precisely.
func Compact(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// Bar renders a fixed-width text meter for a 0..1 ratio.
func Bar(ratio float64, width int) string {
	if width <= 0 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * float64(width)))
	return strings.Repeat("|", filled) + strings.Repeat(" ", width-filled)
}

// Truncate shortens s to width, marking elision with an ellipsis.
func Truncate(s string, width int) string {
	r := []rune(s)
	if width <= 0 {
		return ""
	}
	if len(r) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}
