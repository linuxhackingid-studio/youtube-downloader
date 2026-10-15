package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	lg "charm.land/lipgloss/v2"
)

// RenderHelp renders a muted single-line key legend such as
// "enter: start  •  space: toggle". Each pair is {key, description}.
func RenderHelp(pairs ...[2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, p[0]+": "+p[1])
	}
	return StyleMuted.Render(strings.Join(parts, "  •  "))
}

// RenderBox draws body inside a rounded border with title embedded in the
// top edge. width is the total rendered width and is clamped to a minimum
// of 20 columns; every body line is truncated to fit and space-padded so
// the right border stays straight.
func RenderBox(title, body string, width int) string {
	if width < 20 {
		width = 20
	}

	border := lg.RoundedBorder()
	inner := width - 2 // columns between the vertical borders

	// Top edge: "╭─ title ────╮".
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

	// Body: "│ line │" for every line, padded to the interior width.
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

// Checkbox renders an ASCII checkbox marker: "[x]" when checked, "[ ]"
// otherwise. ASCII is a deliberate choice for maximum terminal support.
func Checkbox(checked bool) string {
	if checked {
		return "[x]"
	}
	return "[ ]"
}

// isControlRune reports whether r is a terminal control code point that must
// never reach the terminal: C0 controls other than '\t' and '\n', DEL, and
// the C1 range U+0080-U+009F (8-bit CSI/OSC/... introducers).
func isControlRune(r rune) bool {
	return (r < 0x20 && r != '\t' && r != '\n') || r == 0x7F || (r >= 0x80 && r <= 0x9F)
}

// isControlByte reports whether the byte b is a control byte when it stands
// alone in the input (i.e. as part of an invalid UTF-8 sequence). Valid UTF-8
// continuation bytes are never passed here, so text such as "€" (E2 82 AC)
// is preserved.
func isControlByte(b byte) bool {
	return (b < 0x20 && b != '\t' && b != '\n') || b == 0x7F || (b >= 0x80 && b <= 0x9F)
}

// Sanitize strips terminal control characters: C0 (0x00-0x1F) except '\t'
// and '\n', DEL (0x7F), and C1 (0x80-0x9F), including raw control bytes that
// arrive via invalid UTF-8 (e.g. a bare 0x9B). The remainder of any ECMA-48
// escape sequence introduced by ESC (0x1B) is removed as well, so text like
// "\x1b[31mred\x1b[0m" yields "red" rather than visible "[31m" fragments.
// Unicode text is preserved. The fast path returns s unchanged when clean.
func Sanitize(s string) string {
	clean := true
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if isControlByte(s[i]) {
				clean = false
				break
			}
			i++
			continue
		}
		if isControlRune(r) {
			clean = false
			break
		}
		i += size
	}
	if clean {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if !isControlByte(s[i]) {
				b.WriteByte(s[i])
			}
			i++
			continue
		}
		if r == 0x1B {
			i = skipEscapeSequence(s, i)
			continue
		}
		if !isControlRune(r) {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// skipEscapeSequence consumes the ECMA-48 escape sequence starting at s[i]
// (an ESC byte) and returns the index just past it. Malformed or truncated
// sequences are consumed up to the end of the string so that no stray
// sequence bytes survive; printable text after the sequence is left intact.
func skipEscapeSequence(s string, i int) int {
	i++ // the ESC introducer itself
	if i >= len(s) {
		return i
	}

	switch c := s[i]; {
	case c == '[': // CSI
		return skipCSIBody(s, i+1)
	case c == ']' || c == 'P' || c == '^' || c == '_' || c == 'X':
		// OSC/DCS/PM/APC/SOS: consume until BEL, ST (ESC \) or raw ST (0x9C).
		i++
		for i < len(s) {
			switch s[i] {
			case 0x07, 0x9C:
				return i + 1
			case 0x1B:
				if i+1 < len(s) && s[i+1] == '\\' {
					return i + 2
				}
			}
			i++
		}
		return i
	case c >= 0x20 && c <= 0x2F:
		// Intermediate byte (e.g. charset designation ESC ( B).
		i++
		if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7E {
			i++
		}
		return i
	default:
		return i // single-character escape: drop the ESC alone
	}
}

// skipCSIBody consumes the body of a CSI sequence starting at s[i]:
// parameter bytes 0x30-0x3F, intermediate bytes 0x20-0x2F, then one final
// byte 0x40-0x7E.
func skipCSIBody(s string, i int) int {
	for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3F {
		i++
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2F {
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7E {
		i++
	}
	return i
}

// Truncate shortens s so that its display width is at most w, replacing
// the removed tail with an ellipsis. The input is first passed through
// Sanitize and newlines/tabs are collapsed to single spaces, because every
// caller renders on a single line. It is rune-width aware; w <= 1
// yields the empty string.
func Truncate(s string, w int) string {
	if w <= 1 {
		return ""
	}

	s = Sanitize(s)
	if strings.ContainsAny(s, "\n\t") {
		s = strings.NewReplacer("\n", " ", "\t", " ").Replace(s)
	}

	if lg.Width(s) <= w {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	cur := 0
	for _, r := range s {
		rw := 1
		if r >= 0x80 {
			rw = lg.Width(string(r))
		}
		if cur+rw > w-1 {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	b.WriteRune('…')
	return b.String()
}

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
