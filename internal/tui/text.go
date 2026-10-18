package tui

import (
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	lg "charm.land/lipgloss/v2"
)

// runeWidthCacheMax bounds the rune-width table: every rune below it gets a
// cached answer, larger runes go through runeWidthWide. 1<<16 covers Latin,
// Greek, Cyrillic, CJK and the Hangul syllables with room to spare, which is
// exactly where the per-rune measurement shows up.
const runeWidthCacheMax = 1 << 16

// runeWidthTable memoizes the display width of one rune: an entry stores
// width+1, so 0 means "not measured yet" and a genuinely zero-width rune (a
// combining mark) is cached as 1 instead of looking unmeasured forever. The
// entries are atomic so the table is race-free even if callers render in
// parallel - the TUI itself renders on a single goroutine, and a duplicate
// measurement merely stores the same value twice.
var runeWidthTable [runeWidthCacheMax]atomic.Uint32

// runeWidthWide memoizes the width of the runes at or above runeWidthCacheMax:
// emoji, less common CJK extensions and other astral-plane codepoints. They are
// far too sparse for the dense table, but measuring one allocates (lg.Width
// converts the rune to a string), and a title that repeats the same emoji on
// every row would pay that allocation on every frame. The key's presence marks
// a rune as measured, so a genuine zero width caches as itself here. Like the
// table, the map is safe for concurrent callers: a duplicate load-and-store
// only recomputes the same width.
var runeWidthWide sync.Map // rune -> int

// runeWidth returns the number of terminal columns r occupies. ASCII is
// always one column, which keeps the hot path free of both a table lookup and
// the heap allocation lg.Width(string(r)) would make. Control runes are the
// callers' concern: Sanitize strips them before any width is measured.
func runeWidth(r rune) int {
	if r < 0x80 {
		return 1
	}
	if r < runeWidthCacheMax {
		if w := runeWidthTable[r].Load(); w != 0 {
			return int(w) - 1
		}
		w := lg.Width(string(r))
		runeWidthTable[r].Store(uint32(w) + 1)
		return w
	}
	if w, ok := runeWidthWide.Load(r); ok {
		return w.(int)
	}
	w := lg.Width(string(r))
	runeWidthWide.Store(r, w)
	return w
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

// SanitizeLines replaces every line of lines with its sanitized form, in place,
// and returns the slice. Callers use it before handing untrusted multi-line
// text to a panel body: frame() only sanitizes a line it has to truncate, and
// lg.Width measures ANSI escape sequences as zero columns, so a short line
// carrying an OSC/CSI sequence would otherwise reach the terminal verbatim.
// Newlines are preserved by construction, because each line is sanitized on its
// own.
func SanitizeLines(lines []string) []string {
	for i, line := range lines {
		lines[i] = Sanitize(line)
	}
	return lines
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
		rw := runeWidth(r)
		if cur+rw > w-1 {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	b.WriteRune('…')
	return b.String()
}
