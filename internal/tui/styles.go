package tui

import (
	"image/color"

	lg "charm.land/lipgloss/v2"
)

// Theme engine for the whole TUI. Both palettes are conservative 256-color
// indexes, so they degrade gracefully (or disappear entirely) on terminals
// with limited color support.
//
// ApplyTheme rebuilds every style variable from the selected palette. Screens
// read those package-level variables at render time and the TUI renders on a
// single goroutine, so a rebuild is picked up by the next frame without any
// synchronization. The redesigned app (T7) calls ApplyTheme from its
// tea.BackgroundColorMsg handler, which is the reply to
// tea.RequestBackgroundColor; until the terminal answers, the dark palette is
// the default.

// palette lists the color indexes of one theme. The values are tuned for
// contrast against the matching terminal background.
type palette struct {
	accent    string // primary accent: titles, key legends, focused borders
	accentAlt string // far end of the progress gradient
	err       string // failures
	ok        string // success
	muted     string // faint background text
	dim       string // descriptions and secondary text
	border    string // unfocused panel borders
	heading   string // headings and panel titles
	value     string // measured values (counts, sizes, times)
}

// The two palettes; every index stays below 256.
var (
	darkPalette = palette{
		accent: "205", accentAlt: "141", err: "196", ok: "82",
		muted: "240", dim: "245", border: "238", heading: "252", value: "248",
	}
	lightPalette = palette{
		accent: "127", accentAlt: "91", err: "160", ok: "28",
		muted: "245", dim: "241", border: "250", heading: "235", value: "236",
	}
)

// Shared styles for every TUI screen, rebuilt by ApplyTheme. The legacy five
// keep their names and types so the existing screens compile unchanged.
var (
	StyleTitle   lg.Style
	StyleMuted   lg.Style
	StyleError   lg.Style
	StyleSuccess lg.Style
	StyleAccent  lg.Style

	StyleHeading     lg.Style // panel titles and section headings
	StyleKey         lg.Style // key names in legends
	StyleDesc        lg.Style // key descriptions in legends
	StyleBorder      lg.Style // quiet border of an unfocused panel
	StyleBorderFocus lg.Style // accented border of a focused panel
	StyleValue       lg.Style // measured values
)

// Active theme state, rebuilt by ApplyTheme. The color.Color values are the
// resolved palette entries the progress widget blends its gradient from.
var (
	themeDark    = true
	colAccent    color.Color
	colAccentAlt color.Color
	colSuccess   color.Color
)

// ApplyTheme selects the dark or light palette and rebuilds every style
// variable from it. It is safe to call at any time between renders. T7 calls
// it from the tea.BackgroundColorMsg handler.
//
// Components that copy a style at construction time (a bubbles spinner built
// with WithStyle, for example) keep the palette they were built with; rebuild
// those components when the theme flips.
func ApplyTheme(dark bool) {
	p := lightPalette
	if dark {
		p = darkPalette
	}
	themeDark = dark

	colAccent = lg.Color(p.accent)
	colAccentAlt = lg.Color(p.accentAlt)
	colSuccess = lg.Color(p.ok)

	StyleTitle = lg.NewStyle().Bold(true).Foreground(colAccent)
	StyleMuted = lg.NewStyle().Faint(true).Foreground(lg.Color(p.muted))
	StyleError = lg.NewStyle().Foreground(lg.Color(p.err))
	StyleSuccess = lg.NewStyle().Foreground(colSuccess)
	StyleAccent = lg.NewStyle().Foreground(colAccent)

	StyleHeading = lg.NewStyle().Bold(true).Foreground(lg.Color(p.heading))
	StyleKey = lg.NewStyle().Bold(true).Foreground(colAccent)
	StyleDesc = lg.NewStyle().Foreground(lg.Color(p.dim))
	StyleBorder = lg.NewStyle().Foreground(lg.Color(p.border))
	StyleBorderFocus = lg.NewStyle().Foreground(colAccent)
	StyleValue = lg.NewStyle().Foreground(lg.Color(p.value))

	// Rebuild the caches that hold strings rendered with the previous
	// palette: the pre-styled download spinner glyphs and the memoized help
	// bar. Every other cache (the pick row cache, the download frame) is
	// invalidated by the app's theme handler.
	rebuildSpinGlyphs()
	resetHelpMemo()
}

// ThemeIsDark reports whether the dark palette is currently active.
func ThemeIsDark() bool { return themeDark }

func init() {
	ApplyTheme(true)
}
