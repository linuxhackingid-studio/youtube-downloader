package tui

import lg "charm.land/lipgloss/v2"

// Shared styles for every TUI screen. Colors are conservative 256-color
// indexes so they degrade gracefully (or disappear entirely) on terminals
// with limited color support.
var (
	StyleTitle   lg.Style
	StyleMuted   lg.Style
	StyleError   lg.Style
	StyleSuccess lg.Style
	StyleAccent  lg.Style
)

func init() {
	const (
		accentColor = "205" // magenta/pink accent
		errorColor  = "196" // bright red
		okColor     = "82"  // bright green
		mutedColor  = "240" // dim gray
		subColor    = "245" // light gray
	)

	StyleTitle = lg.NewStyle().Bold(true).Foreground(lg.Color(accentColor))
	StyleMuted = lg.NewStyle().Faint(true).Foreground(lg.Color(mutedColor))
	StyleError = lg.NewStyle().Foreground(lg.Color(errorColor))
	StyleSuccess = lg.NewStyle().Foreground(lg.Color(okColor))
	StyleAccent = lg.NewStyle().Foreground(lg.Color(accentColor))
}
