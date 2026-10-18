package tui

import "fmt"

// Human-readable formatting helpers shared by every screen. Negative inputs
// are the "unknown" sentinel for the metrics the download workers report, so
// they render as a bare dash instead of nonsense values.

// FormatBytes renders a byte count using binary units with one decimal,
// e.g. "512.0 B", "12.4 MiB". Negative values render as "-".
func FormatBytes(b float64) string {
	if b < 0 {
		return "-"
	}

	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%.1f B", b)
	}

	div := float64(unit)
	exp := 0
	for n := b / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", b/div, "KMGT"[exp])
}

// FormatSpeed renders a transfer rate, e.g. "2.5 MiB/s". Negative values
// render as "-".
func FormatSpeed(bps float64) string {
	if bps < 0 {
		return "-"
	}
	return FormatBytes(bps) + "/s"
}

// FormatETA renders a remaining-time duration as "1h 02m", "2m 05s" or
// "45s". Negative values render as "-".
func FormatETA(secs float64) string {
	if secs < 0 {
		return "-"
	}

	total := int(secs + 0.5)
	h := total / 3600
	m := total % 3600 / 60
	s := total % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// FormatPercent renders a percentage right-aligned in three columns, e.g.
// " 42%" or "100%". Negative values render as " --%".
func FormatPercent(p float64) string {
	if p < 0 {
		return " --%"
	}
	return fmt.Sprintf("%3.0f%%", p)
}
