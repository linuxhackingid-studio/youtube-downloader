package tui

import (
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"
)

// TestBarClampsPercent pins the fill arithmetic and the unknown-percentage
// contract: negative values stay empty and say " --%", values above 100 are a
// complete bar reading "100%".
func TestBarClampsPercent(t *testing.T) {
	tests := []struct {
		name       string
		pct        float64
		wantFilled int
		wantHollow int
		wantPct    string
	}{
		{"unknown is hollow", -1, 0, 10, " --%"},
		{"zero is hollow", 0, 0, 10, "  0%"},
		{"half is half filled", 50, 5, 5, " 50%"},
		{"complete is full", 100, 10, 0, "100%"},
		{"above hundred clamps", 150, 10, 0, "100%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// width 40 - 30 gives the minimum ten bar cells.
			out := Bar("overall", tt.pct, 40)

			if got := strings.Count(out, barFill); got != tt.wantFilled {
				t.Errorf("Bar(%v, 40) filled cells = %d, want %d", tt.pct, got, tt.wantFilled)
			}
			if got := strings.Count(out, barHollow); got != tt.wantHollow {
				t.Errorf("Bar(%v, 40) hollow cells = %d, want %d", tt.pct, got, tt.wantHollow)
			}
			if got := plainText(out); !strings.HasSuffix(got, tt.wantPct) {
				t.Errorf("Bar(%v, 40) = %q, want it to end with %q", tt.pct, got, tt.wantPct)
			}
			if got := lg.Width(out); got != 24 {
				t.Errorf("Bar(%v, 40) width = %d, want 24 (8 label + 1 + 10 cells + 1 + 4 percent)", tt.pct, got)
			}
		})
	}
}

// TestBarClampsWidth pins the 10..60 cell clamp inherited from the legacy
// barLine, so the bar stays usable next to the label in any terminal.
func TestBarClampsWidth(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		wantCells int
	}{
		{"narrow floors at ten cells", 5, 10},
		{"mid width maps one to one", 50, 20},
		{"wide caps at sixty cells", 200, 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Bar("x", 50, tt.width)
			if got := strings.Count(out, barFill) + strings.Count(out, barHollow); got != tt.wantCells {
				t.Errorf("Bar(x, 50, %d) bar cells = %d, want %d", tt.width, got, tt.wantCells)
			}
		})
	}
}

// TestBarCompleteHasNoHollow checks the success contract: a finished bar is
// solid, no hollow remainder is left behind.
func TestBarCompleteHasNoHollow(t *testing.T) {
	out := Bar("overall", 100, 40)

	if strings.Contains(out, barHollow) {
		t.Errorf("Bar(100) = %q, want no hollow cells", plainText(out))
	}
	if got := strings.Count(out, barFill); got != 10 {
		t.Errorf("Bar(100) filled cells = %d, want 10", got)
	}
}

// TestMiniBar pins the compact per-job bar: exact cell count, gradient fill,
// hollow remainder, and the unknown sentinel.
func TestMiniBar(t *testing.T) {
	full := MiniBar(100, 12)
	if got := strings.Count(full, barFill); got != 12 {
		t.Errorf("MiniBar(100, 12) filled = %d, want 12", got)
	}
	if strings.Contains(full, barHollow) {
		t.Errorf("MiniBar(100, 12) = %q, want no hollow cells", plainText(full))
	}
	if got := lg.Width(full); got != 12 {
		t.Errorf("MiniBar(100, 12) width = %d, want 12", got)
	}

	half := MiniBar(50, 12)
	if got, want := strings.Count(half, barFill), 6; got != want {
		t.Errorf("MiniBar(50, 12) filled = %d, want %d", got, want)
	}
	if got, want := strings.Count(half, barHollow), 6; got != want {
		t.Errorf("MiniBar(50, 12) hollow = %d, want %d", got, want)
	}

	unknown := MiniBar(-1, 12)
	if got := strings.Count(unknown, barFill); got != 0 {
		t.Errorf("MiniBar(-1, 12) filled = %d, want 0 for an unknown percentage", got)
	}
	if got := strings.Count(unknown, barHollow); got != 12 {
		t.Errorf("MiniBar(-1, 12) hollow = %d, want 12", got)
	}

	// The width is clamped so a bad caller cannot ask for an unbounded bar.
	if got := lg.Width(MiniBar(50, 500)); got != barMaxCells {
		t.Errorf("MiniBar(50, 500) width = %d, want %d", got, barMaxCells)
	}
	if got := lg.Width(MiniBar(50, 0)); got != 1 {
		t.Errorf("MiniBar(50, 0) width = %d, want 1", got)
	}
}

// TestRampCacheReusesSlice pins the memoization: the same (cells, theme) key
// hands back the same slice, a new theme builds its own, and the ramp really
// is a gradient.
func TestRampCacheReusesSlice(t *testing.T) {
	defer ApplyTheme(true) // leave the package on its default theme

	first := rampFor(20)
	if len(first.colors) != 20 || len(first.cells) != 20 {
		t.Fatalf("rampFor(20) has %d colors and %d cells, want 20 of each", len(first.colors), len(first.cells))
	}
	if first.colors[0] == first.colors[len(first.colors)-1] {
		t.Error("ramp ends are equal, want an accent gradient")
	}

	second := rampFor(20)
	if &first.colors[0] != &second.colors[0] {
		t.Error("rampFor(20) rebuilt the colors, want the memoized slice")
	}
	if &first.cells[0] != &second.cells[0] {
		t.Error("rampFor(20) re-rendered the cells, want the memoized slice")
	}

	ApplyTheme(false)
	light := rampFor(20)
	if ThemeIsDark() {
		t.Error("ThemeIsDark() = true after ApplyTheme(false)")
	}
	if &light.colors[0] == &first.colors[0] {
		t.Error("rampFor(20) reused the dark gradient under the light theme")
	}
	if light.colors[0] == first.colors[0] {
		t.Error("light gradient starts on the dark accent color")
	}
}
