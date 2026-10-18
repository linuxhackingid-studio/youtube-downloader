package tui

import (
	"strings"
	"sync"

	lg "charm.land/lipgloss/v2"
)

// Key legends. The separator is the same bullet the legacy RenderHelp used,
// and the overflow marker mirrors the ellipsis Truncate appends.
const (
	legendSeparator = "  •  "
	legendOverflow  = "  …"
)

// legendEntry renders one {key, description} pair: the key in the key style,
// the description in the description style.
func legendEntry(p [2]string) string {
	return StyleKey.Render(p[0]) + ": " + StyleDesc.Render(p[1])
}

// RenderHelp renders a single-line key legend such as
// "enter: start  •  space: toggle". Each pair is {key, description}. The
// line is rendered in full even when it would overflow the terminal; use
// HelpBar when the width is known.
func RenderHelp(pairs ...[2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, legendEntry(p))
	}
	return strings.Join(parts, StyleMuted.Render(legendSeparator))
}

// helpMemo caches the last legend HelpBar laid out. Callers rebuild the legend
// on every frame - the pick screen even switches between two legends - and the
// layout costs about 30-40us and 100 allocations, so the common case (nothing
// changed since the previous frame) is served from here. The key is the whole
// input: width, active theme and the entry set. ApplyTheme clears it, like
// every other cache that embeds rendered escape sequences.
//
// The mutex is not for the TUI itself, which renders on a single goroutine:
// it makes the memo safe for concurrent callers (tests, future embedders) and
// for a theme flip racing a render.
var helpMemo struct {
	mu    sync.Mutex
	ok    bool
	width int
	dark  bool
	pairs [][2]string // owned copy, reused across calls
	out   string
}

// resetHelpMemo drops the memoized legend so the next HelpBar renders with the
// active palette. ApplyTheme calls it.
func resetHelpMemo() {
	helpMemo.mu.Lock()
	helpMemo.ok = false
	helpMemo.mu.Unlock()
}

// samePairs reports whether two legends hold the same entries in the same
// order. Comparing content (rather than a signature string) keeps the check
// allocation-free.
func samePairs(a, b [][2]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HelpBar lays the key legend out for a terminal of the given width: it keeps
// as many entries as fit, drops the rest, and appends a muted ellipsis when
// entries were dropped. The result never exceeds width columns (measured
// with the ANSI-aware lg.Width, so styling does not count against it). Results
// are memoized by (width, theme, entry set); see helpMemo.
func HelpBar(width int, pairs ...[2]string) string {
	if width <= 0 || len(pairs) == 0 {
		return ""
	}
	dark := ThemeIsDark()

	helpMemo.mu.Lock()
	if helpMemo.ok && helpMemo.width == width && helpMemo.dark == dark &&
		samePairs(helpMemo.pairs, pairs) {
		out := helpMemo.out
		helpMemo.mu.Unlock()
		return out
	}
	helpMemo.mu.Unlock()

	out := buildHelpBar(width, pairs)

	helpMemo.mu.Lock()
	if cap(helpMemo.pairs) < len(pairs) {
		helpMemo.pairs = make([][2]string, len(pairs))
	}
	helpMemo.pairs = helpMemo.pairs[:len(pairs)]
	copy(helpMemo.pairs, pairs)
	helpMemo.width, helpMemo.dark, helpMemo.out, helpMemo.ok = width, dark, out, true
	helpMemo.mu.Unlock()
	return out
}

// buildHelpBar is the actual layout behind HelpBar.
func buildHelpBar(width int, pairs [][2]string) string {
	sep := StyleMuted.Render(legendSeparator)
	sepW := lg.Width(sep)
	overflow := StyleMuted.Render(legendOverflow)
	overflowW := lg.Width(overflow)

	var b strings.Builder
	used, shown := 0, 0
	for i, p := range pairs {
		entry := legendEntry(p)

		add := lg.Width(entry)
		if shown > 0 {
			add += sepW
		}
		// While entries remain after this one, keep room for the overflow
		// marker so a dropped entry always leaves a visible hint behind.
		need := add
		if i < len(pairs)-1 {
			need += overflowW
		}
		if used+need > width {
			break
		}

		if shown > 0 {
			b.WriteString(sep)
		}
		b.WriteString(entry)
		used += add
		shown++
	}

	if shown == 0 {
		return ""
	}
	if shown < len(pairs) && used+overflowW <= width {
		b.WriteString(overflow)
	}
	return b.String()
}
