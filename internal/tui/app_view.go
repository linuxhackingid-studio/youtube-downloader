// app_view.go renders the application frame around the active screen: window
// title, header with the screen context, the screen body itself, the key
// legend footer, and the cancel-confirmation overlay. The frame owns the
// terminal geometry, so no screen has to worry about centering or about
// spilling past the right edge.
package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// appName is the program name shown in the header and the window title.
const appName = "lhi-youtube-downloader"

// Layout constants.
const (
	// maxContentWidth bounds the width the screen bodies are laid out for so
	// lines stay readable on very wide terminals. The frame never exceeds the
	// terminal, so on a narrower terminal the content uses the space that is
	// left, down to the panel floor (minBoxColumns).
	maxContentWidth = 100

	// chromeLines is how many rows the frame spends outside the body: the
	// two header lines, the blank line before and after the body, and the
	// footer legend.
	chromeLines = 5

	// confirmBody is the text of the cancel confirmation overlay.
	confirmBody = "y = cancel everything    n = keep downloading"
)

// View renders the whole application: chrome, the active screen body, and,
// while the cancel confirmation is armed on the download screen, the
// confirmation box.
func (m appModel) View() tea.View {
	width, height := m.viewport()

	// The body is rendered first: the download screen refreshes the metrics
	// the window title reports while it builds its frame.
	body := m.body(width, height)

	v := tea.NewView(m.frame(width, body))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.windowTitle()
	return v
}

// viewport returns the terminal size, falling back to the defaults before the
// first tea.WindowSizeMsg arrives.
func (m appModel) viewport() (width, height int) {
	width, height = m.width, m.height
	if width <= 0 {
		width = defaultViewWidth
	}
	if height <= 0 {
		height = defaultViewHeight
	}
	return width, height
}

// contentWidth maps the terminal width to the width the screen bodies are
// laid out for: at most maxContentWidth so lines stay readable on very wide
// terminals, and never more than the terminal itself so nothing wraps.
func contentWidth(termWidth int) int {
	if termWidth <= 0 {
		termWidth = defaultViewWidth
	}
	if termWidth > maxContentWidth {
		return maxContentWidth
	}
	if termWidth < minBoxColumns {
		return minBoxColumns
	}
	return termWidth
}

// body renders the active screen at the content width. height is the number
// of rows left for it after the frame chrome.
func (m appModel) body(width, height int) string {
	bw := contentWidth(width)
	bh := max(height-chromeLines, 1)

	switch m.screen {
	case scrCheck:
		return m.check.View(bw)
	case scrForm:
		return m.form.View(bw, bh)
	case scrLoading:
		return m.loading.View(bw)
	case scrPick:
		return m.pick.View(bw, bh)
	case scrDownload:
		return m.dl.View(bw, bh)
	case scrDone:
		return m.done.View(bw, bh)
	}
	return ""
}

// frame assembles the chrome around body and centers the result horizontally.
// Every line is clipped to the terminal width and the block is clipped to the
// terminal height, so an oversized body degrades into truncated lines instead
// of wrapping the layout apart.
func (m appModel) frame(width int, body string) string {
	cw := contentWidth(width)

	divider := StyleBorder.Render(strings.Repeat("─", cw))
	header := StyleTitle.Render(appName) + StyleMuted.Render("  "+screenName(m.screen))
	footer := HelpBar(cw, screenHelp(&m)...)

	content := strings.Join([]string{header, divider, "", body, "", footer}, "\n")

	if m.confirmCancel && m.screen == scrDownload {
		content += "\n\n" + RenderBox("Cancel all downloads?", confirmBody, min(56, max(width-4, minBoxColumns)))
	}

	if m.height > 0 {
		content = clampLines(content, m.height, width)
	}
	return lg.PlaceHorizontal(width, lg.Center, content)
}

// windowTitle is the title reported to the terminal. The download screen
// carries live progress so the title bar doubles as a progress indicator when
// the window is in the background.
func (m appModel) windowTitle() string {
	if m.screen == scrDownload {
		return m.dl.Title()
	}
	return appName
}

// screenName returns the human-readable name of a screen for the header.
func screenName(s screen) string {
	switch s {
	case scrCheck:
		return "setup check"
	case scrForm:
		return "configuration"
	case scrLoading:
		return "loading playlist"
	case scrPick:
		return "select videos"
	case scrDownload:
		return "downloading"
	case scrDone:
		return "finished"
	default:
		return ""
	}
}

// screenHelp returns the key legend for a screen. The pick legend depends on
// the live model, because the search field owns the keyboard while it is
// focused; every other legend is static.
func screenHelp(m *appModel) [][2]string {
	switch m.screen {
	case scrCheck:
		return [][2]string{
			{"enter", "continue"},
			{"r", "re-check tools"},
			{"q", "quit"},
		}
	case scrForm:
		return [][2]string{
			{"tab/up/down", "move"},
			{"←/→", "change selector"},
			{"enter", "next field / fetch playlist"},
			{"esc", "back"},
		}
	case scrLoading:
		return [][2]string{
			{"esc", "cancel fetch"},
			{"q", "quit"},
		}
	case scrPick:
		if m.pick.Searching() {
			return [][2]string{
				{"type", "filter title or id"},
				{"enter", "keep filter"},
				{"esc", "clear filter and exit search"},
			}
		}
		return [][2]string{
			{"space", "toggle"},
			{"a/n", "all / none"},
			{"/", "search"},
			{"j/k", "move"},
			{"enter", "start download"},
			{"esc", "back"},
		}
	case scrDownload:
		return [][2]string{
			{"esc", "cancel all (asks y/N)"},
		}
	case scrDone:
		return [][2]string{
			{"enter", "quit"},
			{"r", "download more"},
			{"o", "open output folder"},
		}
	}
	return nil
}

// clampLines clips every line of s to at most width columns and keeps at most
// maxLines lines. When rows had to be dropped, the last kept line is replaced
// by a muted marker so the truncation is visible instead of silent.
func clampLines(s string, maxLines, width int) string {
	if maxLines < 1 {
		maxLines = 1
	}
	if width < 1 {
		width = 1
	}

	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		for i, line := range lines {
			lines[i] = clipStyled(line, width)
		}
		return strings.Join(lines, "\n")
	}

	out := make([]string, 0, maxLines)
	for i := 0; i < maxLines-1; i++ {
		out = append(out, clipStyled(lines[i], width))
	}
	out = append(out, StyleMuted.Render(Truncate("… more lines hidden", max(width-1, 4))))
	return strings.Join(out, "\n")
}

// clipStyled shortens a styled line so that its display width is at most w,
// replacing the removed tail with an ellipsis. Unlike Truncate it keeps ANSI
// escape sequences intact (they are copied verbatim and counted as zero
// width) and closes the line with a reset when it clipped a styled string, so
// a truncated line never bleeds its colors into the rest of the frame.
func clipStyled(s string, w int) string {
	if w <= 1 {
		return ""
	}
	if lg.Width(s) <= w {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	cur := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1B {
			j := skipEscapeSequence(s, i)
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		rw := runeWidth(r)
		if cur+rw > w-1 {
			break
		}
		b.WriteString(s[i : i+size])
		cur += rw
		i += size
	}
	b.WriteRune('…')
	if strings.ContainsRune(s, 0x1B) {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
