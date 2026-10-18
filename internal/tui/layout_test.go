package tui

import (
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"
)

// assertBoxWidth splits a rendered box into its lines and fails unless every
// line is exactly width columns wide, which is what proves the right border
// stays straight.
func assertBoxWidth(t *testing.T, out string, width int) []string {
	t.Helper()

	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if got := lg.Width(line); got != width {
			t.Errorf("line %d width = %d, want %d (%q)", i, got, width, plainText(line))
		}
	}
	return lines
}

// TestPanelRendersBodyInsideBorder checks the shape the redesigned screens
// rely on: every body line sits inside the frame, and the frame is exactly
// the requested width.
func TestPanelRendersBodyInsideBorder(t *testing.T) {
	const width = 40

	out := Panel("Details", "first line\nsecond line", width, false)
	lines := assertBoxWidth(t, out, width)

	if len(lines) != 4 {
		t.Fatalf("Panel rendered %d lines, want 4 (top edge, two body rows, bottom edge)", len(lines))
	}
	if !strings.Contains(lines[0], "Details") {
		t.Errorf("top edge = %q, want it to embed the title", plainText(lines[0]))
	}
	for i, want := range []string{"first line", "second line"} {
		if !strings.Contains(lines[i+1], want) {
			t.Errorf("body line %d = %q, want %q inside the border", i, plainText(lines[i+1]), want)
		}
	}
	if pad := plainText(lines[1]); !strings.HasPrefix(pad, "│ ") || !strings.HasSuffix(pad, " │") {
		t.Errorf("body line = %q, want one column of padding inside the borders", pad)
	}
	if bottom := plainText(lines[3]); !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Errorf("bottom edge = %q, want rounded corners", bottom)
	}
}

// TestPanelFocusChangesStyling pins that the focus flag is not cosmetic by
// accident: the focused panel uses a different style while keeping the same
// geometry.
func TestPanelFocusChangesStyling(t *testing.T) {
	focused := Panel("Details", "body", 40, true)
	unfocused := Panel("Details", "body", 40, false)

	if focused == unfocused {
		t.Error("focused and unfocused panels render identically, want the focus state to change the styles")
	}
	if got, want := lg.Width(unfocused), lg.Width(focused); got != want {
		t.Errorf("unfocused width = %d, focused width = %d, want identical layout", got, want)
	}
}

// TestPanelEmptyTitleKeepsFullEdge checks the title-less variant: the top
// edge must close on the last column instead of leaving a gap.
func TestPanelEmptyTitleKeepsFullEdge(t *testing.T) {
	const width = 30

	lines := assertBoxWidth(t, Panel("", "body", width, false), width)
	want := "╭" + strings.Repeat("─", width-2) + "╮"
	if got := plainText(lines[0]); got != want {
		t.Errorf("top edge = %q, want %q", got, want)
	}
}

// TestPanelTruncatesLongTitle checks the truncation safety: an overlong title
// cannot push the top-right corner off the layout width.
func TestPanelTruncatesLongTitle(t *testing.T) {
	const width = 30
	long := strings.Repeat("wide title ", 8)

	lines := assertBoxWidth(t, Panel(long, "body", width, true), width)
	top := plainText(lines[0])

	if strings.Contains(top, long) {
		t.Errorf("top edge = %q, want the overlong title truncated", top)
	}
	if !strings.Contains(top, "…") {
		t.Errorf("top edge = %q, want the truncation ellipsis", top)
	}
}

// TestPanelTruncatesLongBodyLines checks that overlong body lines are clipped
// to the interior width instead of wrapping past the border.
func TestPanelTruncatesLongBodyLines(t *testing.T) {
	const width = 24

	lines := assertBoxWidth(t, Panel("t", strings.Repeat("x", 200), width, false), width)
	want := "│ " + strings.Repeat("x", width-5) + "… │"
	if got := plainText(lines[1]); got != want {
		t.Errorf("body line = %q, want %q", got, want)
	}
}

// TestPanelClampsNarrowWidth pins the 20 column floor shared by Panel and the
// legacy RenderBox, body truncation included.
func TestPanelClampsNarrowWidth(t *testing.T) {
	body := strings.Repeat("b", 50)
	tests := []struct {
		name string
		out  string
	}{
		{"Panel", Panel("title", body, 5, false)},
		{"RenderBox", RenderBox("title", body, 5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := assertBoxWidth(t, tt.out, minBoxColumns)
			if len(lines) != 3 {
				t.Fatalf("rendered %d lines, want 3 (top, one body row, bottom)", len(lines))
			}
			if !strings.Contains(plainText(lines[1]), "…") {
				t.Errorf("body line = %q, want the body truncated at the floor width", plainText(lines[1]))
			}
		})
	}
}

// TestCheckboxMarkers pins the legacy ASCII checkbox contract.
func TestCheckboxMarkers(t *testing.T) {
	if got := Checkbox(true); got != "[x]" {
		t.Errorf("Checkbox(true) = %q, want %q", got, "[x]")
	}
	if got := Checkbox(false); got != "[ ]" {
		t.Errorf("Checkbox(false) = %q, want %q", got, "[ ]")
	}
}

// legacyRenderBox is the pre-redesign implementation, kept here as the
// reference the restyled RenderBox is compared against: the visual language
// changed, the geometry and the body text must not.
func legacyRenderBox(title, body string, width int) string {
	if width < 20 {
		width = 20
	}

	border := lg.RoundedBorder()
	inner := width - 2 // columns between the vertical borders

	label := title
	if maxLabel := inner - 3; lg.Width(label) > maxLabel {
		label = Truncate(label, maxLabel)
	}
	fill := max(inner-3-lg.Width(label), 0)

	var out strings.Builder
	out.Grow(width * (strings.Count(body, "\n") + 3))
	out.WriteString(border.TopLeft)
	out.WriteString(border.Top)
	out.WriteByte(' ')
	out.WriteString(label)
	out.WriteByte(' ')
	out.WriteString(strings.Repeat(border.Top, fill))
	out.WriteString(border.TopRight)

	textW := inner - 2
	for _, raw := range strings.Split(body, "\n") {
		line := raw
		if lg.Width(line) > textW {
			line = Truncate(line, textW)
		}
		out.WriteByte('\n')
		out.WriteString(border.Left)
		out.WriteByte(' ')
		out.WriteString(line)
		out.WriteString(strings.Repeat(" ", max(textW-lg.Width(line), 0)))
		out.WriteByte(' ')
		out.WriteString(border.Right)
	}

	out.WriteByte('\n')
	out.WriteString(border.BottomLeft)
	out.WriteString(strings.Repeat(border.Bottom, inner))
	out.WriteString(border.BottomRight)

	return out.String()
}

// TestRenderBoxKeepsLegacyGeometry compares the restyled box against the
// pre-redesign implementation for the inputs the screens actually produce:
// same frame, same padding, same truncation, only the colors differ.
func TestRenderBoxKeepsLegacyGeometry(t *testing.T) {
	tests := []struct {
		name  string
		title string
		body  string
		width int
	}{
		{"typical", "Install required tools", "line one\nline two", 56},
		{"floor width", "A", "body", 20},
		{"below floor", "A", "body", 7},
		{"long title", strings.Repeat("wide title ", 8), "body", 30},
		{"wide runes", "x", "世界abc", 24},
		{"long body", "x", strings.Repeat("x", 200), 24},
		{"unicode body", "T", "café €5\nmore", 22},
		{"tab body", "T", "a\tb\nc", 21},
		{"styled body", "T", "\x1b[31mred\x1b[0m", 24},
		{"trailing newline", "T", "line\n", 24},
		{"control bytes", "T", "a\x00b\nc", 21},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := plainText(legacyRenderBox(tt.title, tt.body, tt.width))
			got := plainText(RenderBox(tt.title, tt.body, tt.width))
			if got != want {
				t.Errorf("RenderBox(%q, %q, %d):\n got = %q\nwant = %q",
					tt.title, tt.body, tt.width, got, want)
			}
		})
	}
}
