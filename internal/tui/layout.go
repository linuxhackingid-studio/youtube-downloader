package tui

import (
	"strings"

	lg "charm.land/lipgloss/v2"
)

// minBoxColumns is the narrowest a bordered panel may render: any less and
// the border and the body would have no room left.
const minBoxColumns = 20

// frameStyle bundles the two styles of a bordered panel so the shared
// renderer takes one argument instead of two. It is built at call time, not
// as a package variable, because ApplyTheme replaces the style variables and
// a pre-built bundle would keep the stale ones.
type frameStyle struct {
	border lg.Style
	title  lg.Style
}

// RenderBox draws body inside a rounded border with title embedded in the
// top edge. width is the total rendered width and is clamped to a minimum
// of 20 columns; every body line is truncated to fit and space-padded so
// the right border stays straight. The frame carries the accent color and
// the title the heading style.
//
// RenderBox only truncates: a body line carrying EXTERNAL data (yt-dlp
// output, playlist text) must be sanitized by the caller before it is passed
// here, exactly like Panel - see the contract documented there.
func RenderBox(title, body string, width int) string {
	return frame(title, body, width, frameStyle{border: StyleBorderFocus, title: StyleHeading})
}

// Panel draws body inside a rounded border with title embedded in the top
// edge and one column of horizontal padding. A focused panel uses the accent
// border and the heading style; an unfocused one keeps the quiet border
// color. width is the total rendered width, clamped to a minimum of 20
// columns, and body lines get the same truncate-and-pad treatment as
// RenderBox. An empty title renders an uninterrupted top edge.
//
// Sanitize contract: a body line that carries EXTERNAL data - yt-dlp output,
// a playlist title, anything the program did not render itself - must be
// sanitized by the caller (Sanitize, or SanitizeLines for a multi-line body)
// before it is passed here. frame only truncates a line that overflows the
// body width, and lg.Width measures ANSI escape sequences as zero columns,
// so a short line carrying a CSI or OSC sequence would measure as ordinary
// text and pass through the panel verbatim; Panel does not sanitize for you.
func Panel(title, body string, width int, focused bool) string {
	if focused {
		return frame(title, body, width, frameStyle{border: StyleBorderFocus, title: StyleHeading})
	}
	return frame(title, body, width, frameStyle{border: StyleBorder, title: StyleHeading})
}

// frame is the shared renderer behind RenderBox and Panel: a rounded box of
// exactly width columns with the title embedded in the top edge.
func frame(title, body string, width int, fs frameStyle) string {
	if width < minBoxColumns {
		width = minBoxColumns
	}

	border := lg.RoundedBorder()
	inner := width - 2 // columns between the vertical borders
	textW := inner - 2 // one column of padding on each side of the body

	left := fs.border.Render(border.Left)
	right := fs.border.Render(border.Right)

	var out strings.Builder
	out.Grow(width * (strings.Count(body, "\n") + 3))
	writeTop(&out, title, inner, border, fs)

	for _, raw := range strings.Split(body, "\n") {
		line := raw
		if lg.Width(line) > textW {
			line = Truncate(line, textW)
		}
		out.WriteByte('\n')
		out.WriteString(left)
		out.WriteByte(' ')
		out.WriteString(line)
		out.WriteString(strings.Repeat(" ", max(textW-lg.Width(line), 0)))
		out.WriteByte(' ')
		out.WriteString(right)
	}

	out.WriteByte('\n')
	out.WriteString(fs.border.Render(border.BottomLeft + strings.Repeat(border.Bottom, inner) + border.BottomRight))

	return out.String()
}

// writeTop writes the first line: "╭─ title ────╮" when there is a title,
// "╭────────╮" when there is not. An overlong title is truncated so the
// corner always lands exactly on the last column.
func writeTop(out *strings.Builder, title string, inner int, border lg.Border, fs frameStyle) {
	label := title
	if maxLabel := inner - 3; maxLabel > 0 && lg.Width(label) > maxLabel {
		label = Truncate(label, maxLabel)
	}
	if label == "" {
		out.WriteString(fs.border.Render(border.TopLeft + strings.Repeat(border.Top, inner) + border.TopRight))
		return
	}

	fill := max(inner-3-lg.Width(label), 0)
	out.WriteString(fs.border.Render(border.TopLeft + border.Top))
	out.WriteByte(' ')
	out.WriteString(fs.title.Render(label))
	out.WriteByte(' ')
	out.WriteString(fs.border.Render(strings.Repeat(border.Top, fill) + border.TopRight))
}

// Checkbox renders an ASCII checkbox marker: "[x]" when checked, "[ ]"
// otherwise. ASCII is a deliberate choice for maximum terminal support.
func Checkbox(checked bool) string {
	if checked {
		return "[x]"
	}
	return "[ ]"
}
