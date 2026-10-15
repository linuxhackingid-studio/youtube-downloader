package tui

import (
	"fmt"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// pickModel is the video-selection screen for a fetched playlist. Videos keep
// their absolute playlist index; the optional range filter only decides which
// entries are shown. Everything visible starts selected and the user
// deselects what should not be downloaded.
type pickModel struct {
	videos   []ytdlp.Video // filtered subset, absolute Index preserved
	selected []bool        // parallel to videos; true = queued for download
	cursor   int           // index into videos of the highlighted row

	vp viewport.Model

	hint   string // transient message, rendered in place of the ready counter
	width  int
	height int

	// Range filter that produced videos, kept for the header label.
	filtered   bool
	rangeStart int
	rangeEnd   int

	// rows caches the rendered list rows (parallel to videos) so Update can
	// patch only the rows a key changed instead of rebuilding every row each
	// frame. lastRowW is the width the cache was laid out for; 0 means the
	// cache is stale (or never built) and View must rebuild it.
	rows     []string
	lastRowW int
}

// newPickModel builds the selection screen for pl. rangeStart and rangeEnd
// are inclusive absolute playlist indexes; a zero value disables that bound.
// Videos inside the range are copied in order and start selected.
func newPickModel(pl *ytdlp.Playlist, rangeStart, rangeEnd int) pickModel {
	m := pickModel{
		vp:         viewport.New(viewport.WithWidth(60), viewport.WithHeight(10)),
		filtered:   rangeStart > 0 || rangeEnd > 0,
		rangeStart: rangeStart,
		rangeEnd:   rangeEnd,
	}

	if pl != nil {
		for _, v := range pl.Videos {
			if rangeStart > 0 && v.Index < rangeStart {
				continue
			}
			if rangeEnd > 0 && v.Index > rangeEnd {
				continue
			}
			m.videos = append(m.videos, v)
		}
	}

	m.selected = make([]bool, len(m.videos))
	for i := range m.selected {
		m.selected[i] = true
	}
	return m
}

// Init implements the Bubble Tea model interface; the screen has no startup
// work of its own.
func (m pickModel) Init() tea.Cmd {
	return nil
}

// Update handles window size messages and the selection keys only: up/down
// (and k/j) move the cursor, space toggles the highlighted video, and a/n
// select or clear everything. Space also clears the transient hint. Every
// other message is ignored because navigation beyond this screen is owned by
// the app.
func (m pickModel) Update(msg tea.Msg) (pickModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Record the terminal size and warm the row cache for the new
		// layout width. Returning the rebuilt model keeps rows and
		// lastRowW, so patchRows on later keys patches single rows
		// instead of View rebuilding everything each frame. View still
		// owns the viewport sizing and the cold-start fallback rebuild.
		m.width, m.height = msg.Width, msg.Height
		if len(m.videos) > 0 && msg.Width > 0 {
			m = m.rebuildRows(msg.Width)
		}
		return m, nil

	case tea.KeyPressMsg:
		keyStr := msg.String()
		oldCursor := m.cursor

		switch keyStr {
		case "up", "k":
			m.cursor--
		case "down", "j":
			m.cursor++
		case "space":
			if len(m.videos) > 0 && m.cursor < len(m.selected) {
				m.selected[m.cursor] = !m.selected[m.cursor]
			}
			m.hint = ""
		case "a", "A":
			for i := range m.selected {
				m.selected[i] = true
			}
		case "n", "N":
			for i := range m.selected {
				m.selected[i] = false
			}
		}

		if len(m.videos) == 0 {
			m.cursor = 0
		} else {
			m.cursor = min(max(m.cursor, 0), len(m.videos)-1)
		}

		// Refresh only the cached rows this key affected. Before the first
		// rebuild (lastRowW == 0) there is nothing to patch: View rebuilds the
		// whole cache from scratch on the next frame.
		switch keyStr {
		case "up", "k", "down", "j":
			m = m.patchRows(oldCursor, m.cursor)
		case "space":
			m = m.patchRows(m.cursor)
		case "a", "A", "n", "N":
			if m.lastRowW > 0 {
				m = m.rebuildRows(m.lastRowW)
			}
		}
	}
	return m, nil
}

// renderRow renders one list row for videos[i] at the given content width.
// It is the single source of truth for the row layout, shared by the full
// rebuild and the incremental patches.
func (m pickModel) renderRow(i, width int) string {
	v := m.videos[i]
	prefix := StyleMuted.Render("  ")
	if i == m.cursor {
		prefix = StyleTitle.Render("> ")
	}
	dur := " --:--"
	if v.Duration > 0 {
		dur = FormatETA(v.Duration)
	}
	title := Truncate(v.Title, max(10, width-32))
	return prefix + Checkbox(m.selected[i]) + fmt.Sprintf(" %4d. ", v.Index) + dur + " " + title
}

// rebuildRows rebuilds every cached row from scratch for width and records
// the width used. It is O(N) and only needed when the cache is cold or a key
// touched every row (select all / select none).
func (m pickModel) rebuildRows(width int) pickModel {
	rows := make([]string, len(m.videos))
	for i := range m.videos {
		rows[i] = m.renderRow(i, width)
	}
	m.rows = rows
	m.lastRowW = width
	return m
}

// patchRows re-renders the cached rows at idxs in place, using the width the
// cache was built for. It is a no-op while the cache is stale (lastRowW == 0)
// because View rebuilds everything in that case.
func (m pickModel) patchRows(idxs ...int) pickModel {
	if m.lastRowW <= 0 {
		return m
	}
	for _, i := range idxs {
		if i >= 0 && i < len(m.rows) && i < len(m.videos) {
			m.rows[i] = m.renderRow(i, m.lastRowW)
		}
	}
	return m
}

// View renders the header, the scrollable list of videos, the hint line and
// the key legend. width and height are the terminal dimensions; the viewport
// takes height-5 rows and width-2 columns. An empty selection renders a
// muted placeholder instead of the viewport.
func (m pickModel) View(width, height int) string {
	m.width, m.height = width, height

	total := len(m.videos)
	count := m.SelectedCount()

	header := StyleTitle.Render(fmt.Sprintf("Videos — %d selected of %d", count, total))
	if m.filtered {
		header += StyleMuted.Render(m.rangeLabel())
	}

	help := RenderHelp(
		[2]string{"space", "toggle"},
		[2]string{"a/n", "select all/none"},
		[2]string{"enter", "start download"},
		[2]string{"esc", "back"},
	)

	if total == 0 {
		return header + "\n" + StyleMuted.Render("(no videos)") + "\n" + help
	}

	hintLine := StyleMuted.Render(fmt.Sprintf("%d of %d ready", count, total))
	if m.hint != "" {
		hintLine = StyleError.Render(m.hint)
	}

	h := max(3, height-5)
	w := max(20, width-2)
	if m.vp.Height() != h {
		m.vp.SetHeight(h)
	}
	if m.vp.Width() != w {
		m.vp.SetWidth(w)
	}

	// Rebuild the row cache only when the layout width changed (or the
	// cache was never built); Update patches the affected rows on keys.
	if m.rows == nil || m.lastRowW != width {
		m = m.rebuildRows(width)
	}

	// Content first: SetYOffset clamps against the lines the viewport
	// currently holds, and View works on a receiver copy, so offset
	// adjustments made before SetContentLines would clamp against stale
	// (empty) content and never scroll.
	m.vp.SetContentLines(m.rows)

	// Keep the cursor row inside the viewport window.
	off := m.vp.YOffset()
	vis := m.vp.Height()
	if m.cursor < off {
		m.vp.SetYOffset(m.cursor)
	} else if m.cursor >= off+vis {
		m.vp.SetYOffset(m.cursor - vis + 1)
	}

	return header + "\n" + m.vp.View() + "\n" + hintLine + "\n" + help
}

// Selected returns a copy of every still-selected video, in playlist order.
func (m pickModel) Selected() []ytdlp.Video {
	out := make([]ytdlp.Video, 0, len(m.videos))
	for i, v := range m.videos {
		if i < len(m.selected) && m.selected[i] {
			out = append(out, v)
		}
	}
	return out
}

// SelectedCount reports how many videos are currently selected.
func (m pickModel) SelectedCount() int {
	n := 0
	for i := range m.videos {
		if i < len(m.selected) && m.selected[i] {
			n++
		}
	}
	return n
}

// TotalShown reports how many videos the range filter left visible.
func (m pickModel) TotalShown() int {
	return len(m.videos)
}

// SetHint stores a transient message shown in place of the ready counter.
func (m pickModel) SetHint(s string) pickModel {
	m.hint = s
	return m
}

// rangeLabel renders the header suffix for the active range filter, e.g.
// " (range 3–40)", " (range 3–)" or " (range –40)".
func (m pickModel) rangeLabel() string {
	switch {
	case m.rangeStart > 0 && m.rangeEnd > 0:
		return fmt.Sprintf(" (range %d–%d)", m.rangeStart, m.rangeEnd)
	case m.rangeStart > 0:
		return fmt.Sprintf(" (range %d–)", m.rangeStart)
	case m.rangeEnd > 0:
		return fmt.Sprintf(" (range –%d)", m.rangeEnd)
	default:
		return ""
	}
}
