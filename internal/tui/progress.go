package tui

import (
	"image/color"
	"strings"

	lg "charm.land/lipgloss/v2"
)

// Progress widgets: a labelled full-width bar and a compact per-job bar.
const (
	// barMinCells and barMaxCells bound the cell count of a bar so it stays
	// usable in narrow and wide terminals alike (the same 10..60 clamp the
	// legacy barLine applied to width-30).
	barMinCells = 10
	barMaxCells = 60
	// barLabelCells is the fixed label column of Bar; the rest of the line
	// belongs to the bar and the percentage.
	barLabelCells = 8

	barFill   = "█"
	barHollow = "░"
)

// rampKey identifies a memoized gradient ramp: the number of cells it spans
// and the theme it was built for.
type rampKey struct {
	cells int
	dark  bool
}

// ramp is one memoized gradient: the blended colors plus the pre-rendered
// fill cell of every position, so rendering a frame never has to build a
// lipgloss style once per cell.
type ramp struct {
	colors []color.Color // gradient stops, one per cell
	cells  []string      // pre-rendered barFill per cell
}

// rampCache memoizes gradient ramps keyed by (cells, isDark). The TUI renders
// every frame on the single bubbletea event goroutine, so the map needs no
// lock. The 1..60 cell clamp and the two themes bound it to 120 entries.
var rampCache = make(map[rampKey]ramp)

// rampFor returns the memoized gradient for a bar of the given cell count
// under the active theme. The returned slices are shared and must not be
// modified. It is safe to call on every frame: only the first call for a
// (cells, isDark) pair allocates.
//
// lg.Blend1D blends in CIELAB and hands back RGB colors, so the rendered
// cells carry truecolor sequences even though the palette is 256-color; the
// bubbletea renderer downsamples them to the terminal's own profile.
func rampFor(cells int) ramp {
	key := rampKey{cells: cells, dark: themeDark}
	if cached, ok := rampCache[key]; ok {
		return cached
	}

	colors := lg.Blend1D(cells, colAccent, colAccentAlt)
	rendered := make([]string, len(colors))
	for i, c := range colors {
		rendered[i] = lg.NewStyle().Foreground(c).Render(barFill)
	}

	r := ramp{colors: colors, cells: rendered}
	rampCache[key] = r
	return r
}

// Bar renders a progress line: a label padded to 8 columns, a gradient-filled
// bar, and the percentage. width is the whole line budget; the bar itself is
// clamped to 10..60 cells. A negative percentage is the unknown sentinel: it
// renders an all-hollow bar and " --%" instead of a made-up fill, and
// percentages above 100 clamp to a complete bar reading "100%".
func Bar(label string, pct float64, width int) string {
	cells := barCells(width)
	return padLabel(label) + " " + barBody(pct, cells) + " " + FormatPercent(min(pct, 100))
}

// MiniBar renders a compact bar for a per-job row: the same gradient fill and
// hollow remainder as Bar, without the label or the percentage. A negative
// percentage renders an all-hollow bar, so rows with unknown progress still
// line up with the others. width is clamped to 1..60 cells.
func MiniBar(pct float64, width int) string {
	return barBody(pct, min(max(width, 1), barMaxCells))
}

// barCells maps a whole-line width to the number of bar cells: the label, the
// percentage and the separating spaces take roughly 30 columns.
func barCells(width int) int {
	return min(max(width-30, barMinCells), barMaxCells)
}

// barBody renders the bar itself: filled cells carry the accent gradient, a
// complete bar turns solid success green, and the remainder is hollow.
func barBody(pct float64, cells int) string {
	filled := fillCells(pct, cells)

	var b strings.Builder
	// A gradient cell carries a truecolor sequence, so allow ~26 bytes per
	// cell plus the hollow remainder and the percentage.
	b.Grow(cells*26 + 16)
	if filled > 0 {
		if pct >= 100 {
			b.WriteString(StyleSuccess.Render(strings.Repeat(barFill, filled)))
		} else {
			for _, cell := range rampFor(cells).cells[:filled] {
				b.WriteString(cell)
			}
		}
	}
	if hollow := cells - filled; hollow > 0 {
		b.WriteString(StyleMuted.Render(strings.Repeat(barHollow, hollow)))
	}
	return b.String()
}

// fillCells converts a percentage to a filled cell count, clamped to
// [0, cells]; a negative (unknown) percentage fills nothing.
func fillCells(pct float64, cells int) int {
	if pct < 0 {
		return 0
	}
	return min(max(int(min(pct, 100)/100*float64(cells)), 0), cells)
}

// padLabel renders the fixed-width label column: an overlong label is
// truncated with the shared ellipsis semantics, then padded with spaces so
// consecutive bars line up.
func padLabel(label string) string {
	label = Truncate(label, barLabelCells)
	return label + strings.Repeat(" ", max(barLabelCells-lg.Width(label), 0))
}
