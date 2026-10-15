package tui

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"clean ascii", "hello world 123", "hello world 123"},
		{"tab and newline kept", "a\tb\nc", "a\tb\nc"},
		{"unicode preserved", "héllo→世界 €5", "héllo→世界 €5"},
		{"accented latin and cjk unchanged", "café 世界 OK", "café 世界 OK"},
		{"plain brackets preserved", "array[0] stays [ok]", "array[0] stays [ok]"},
		{"lone esc at end stripped", "\x1b", ""},
		{"osc 52 clipboard sequence stripped", "\x1b]52;c;ZGF0YQ\x07", ""},
		{"esc stripped", "a\x1bb", "ab"},
		{"ansi sgr stripped", "\x1b[31mred\x1b[0m", "red"},
		{"osc stripped", "\x1b]0;evil\x07safe", "safe"},
		{"charset escape stripped", "\x1b(Bplain", "plain"},
		{"truncated escape stripped", "abc\x1b[", "abc"},
		{"c0 controls stripped", "a\x00b\x07c\rd", "abcd"},
		{"del stripped", "a\x7fb", "ab"},
		{"raw c1 byte stripped", "a\x9bb", "ab"},
		{"encoded c1 rune stripped", "a\u009bb", "ab"},
		{"mixed controls with tab and newline", "ok\x1b\x9b\x7f\t\n", "ok\t\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sanitize(tt.in); got != tt.want {
				t.Fatalf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSanitizeEscBeforeUppercaseX pins the contract for an ESC followed by
// 'X': the implementation treats 'X' as a SOS-class sequence introducer and
// consumes the remainder of the string (like OSC without a terminator), so
// only the printable prefix survives. Independently of the exact survivor,
// no control rune may ever reach the terminal.
func TestSanitizeEscBeforeUppercaseX(t *testing.T) {
	const in = "a\x1bXb"

	got := Sanitize(in)

	// Pinned implementation behavior (see skipEscapeSequence in widgets.go):
	// SOS-class introducers run to end of string when no terminator follows,
	// so both the 'X' and the trailing 'b' are consumed here.
	if got != "a" {
		t.Errorf("Sanitize(%q) = %q, pinned implementation behavior is %q", in, got, "a")
	}

	for _, r := range got {
		if r == 0x1B {
			t.Errorf("Sanitize(%q) = %q still contains ESC", in, got)
		}
		if (r < 0x20 && r != '\t' && r != '\n') || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
			t.Errorf("Sanitize(%q) = %q still contains control rune %U", in, got, r)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		w    int
		want string
	}{
		{"exact fit unchanged", "hello", 5, "hello"},
		{"shorter than limit", "hi", 10, "hi"},
		{"truncated with ellipsis", "hello world", 5, "hell…"},
		{"ellipsis only", "hello", 2, "h…"},
		{"width one", "hello", 1, ""},
		{"width zero", "hello", 0, ""},
		{"negative width", "hello", -3, ""},
		{"wide runes truncate mid-rune", "世界abc", 3, "世…"},
		{"wide runes keep two then ellipsis", "世界abc", 5, "世界…"},
		{"wide runes exact fit", "世界", 4, "世界"},
		{"newline becomes space", "a\nb", 10, "a b"},
		{"tab becomes space", "a\tb", 10, "a b"},
		{"newline collapsed before truncation", "ab\ncd", 4, "ab …"},
		{"esc stripped untruncated", "\x1b[31mred\x1b[0m", 20, "red"},
		{"esc stripped then truncated", "ab\x1b[31mcd", 3, "ab…"},
		{"ansi digits sanitized then truncated", "12\x1b[31m345\x1b[0m", 4, "123…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Truncate(tt.in, tt.w); got != tt.want {
				t.Fatalf("Truncate(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
			}
		})
	}
}
