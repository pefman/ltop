// Package format renders numbers and meters for terminal display.
package format

import (
	"fmt"
	"math"
	"strings"
	"time"
)

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
