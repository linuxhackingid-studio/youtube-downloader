package tui

import (
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"
)

// plainText strips ANSI escape sequences so tests can assert on the visible
// text of a styled string instead of raw escape bytes. It understands the CSI
// sequences lipgloss emits and passes everything else through untouched, so
// it is a no-op when a build renders without colors.
func plainText(s string) string {
	if !strings.ContainsRune(s, 0x1B) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3F {
				i++ // parameter and intermediate bytes
			}
			if i < len(s) {
				i++ // the final byte
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// helpPairs is the legend the HelpBar tests lay out: short entries whose
// widths make the fitting arithmetic easy to follow.
func helpPairs() [][2]string {
	return [][2]string{
		{"enter", "open"},
		{"space", "toggle"},
		{"a", "all"},
		{"n", "none"},
		{"esc", "back"},
		{"?", "help"},
	}
}

// TestRenderHelpJoinsWithBullet pins the legacy single-line legend contract:
// every key is followed by its description, and the bullet separator sits
// between the entries.
func TestRenderHelpJoinsWithBullet(t *testing.T) {
	out := RenderHelp([2]string{"enter", "start"}, [2]string{"space", "toggle"})

	if strings.Contains(out, "\n") {
		t.Errorf("RenderHelp = %q, want a single line", out)
	}
	if !strings.Contains(out, legendSeparator) {
		t.Errorf("RenderHelp = %q, want the %q separator inside the output", out, legendSeparator)
	}
	want := "enter: start" + legendSeparator + "space: toggle"
	if got := plainText(out); got != want {
		t.Errorf("RenderHelp = %q, want %q", got, want)
	}
}

// TestHelpBarFitsWidth checks the promise that matters for the redesigned
// screens: the legend never spills past the terminal width.
func TestHelpBarFitsWidth(t *testing.T) {
	pairs := helpPairs()

	for _, width := range []int{20, 40, 80} {
		out := HelpBar(width, pairs...)
		if out == "" {
			t.Fatalf("HelpBar(%d) is empty, want at least the first entry", width)
		}
		if got := lg.Width(out); got > width {
			t.Errorf("HelpBar(%d) width = %d, want <= %d (%q)", width, got, width, plainText(out))
		}
	}
}

// TestHelpBarDropsOverflow pins the graceful degradation: narrow widths keep
// the leading entries and mark the omission, wide widths keep everything.
func TestHelpBarDropsOverflow(t *testing.T) {
	pairs := helpPairs()

	narrow := HelpBar(20, pairs...)
	if got := lg.Width(narrow); got > 20 {
		t.Errorf("HelpBar(20) width = %d, want <= 20", got)
	}
	if !strings.Contains(narrow, "…") {
		t.Errorf("HelpBar(20) = %q, want the overflow marker", plainText(narrow))
	}
	if !strings.Contains(narrow, "enter") {
		t.Errorf("HelpBar(20) = %q, want the leading entry kept", plainText(narrow))
	}
	if strings.Contains(narrow, "help") {
		t.Errorf("HelpBar(20) = %q, want the trailing entry dropped", plainText(narrow))
	}

	wide := HelpBar(80, pairs...)
	if strings.Contains(wide, "…") {
		t.Errorf("HelpBar(80) = %q, want no overflow marker", plainText(wide))
	}
	for _, p := range pairs {
		if !strings.Contains(wide, p[0]) || !strings.Contains(wide, p[1]) {
			t.Errorf("HelpBar(80) = %q, missing legend entry %v", plainText(wide), p)
		}
	}

	// Nothing fits at all: an empty string is better than a clipped entry.
	if out := HelpBar(5, pairs...); out != "" {
		t.Errorf("HelpBar(5) = %q, want empty when no entry fits", plainText(out))
	}
}
