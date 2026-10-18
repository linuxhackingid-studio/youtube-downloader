package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// pickMouseScrollLines is how many rows one mouse-wheel notch moves the
// cursor; most terminals send three lines per notch.
const pickMouseScrollLines = 3

// Bounds for the inline search field, so it stays readable from a 40-column
// terminal up to a very wide one.
const (
	pickSearchMinWidth = 20
	pickSearchMaxWidth = 40
)

// pickModel is the video-selection screen for a fetched playlist. Videos keep
// their absolute playlist index; the optional range filter only decides which
// entries are offered, and the optional search filter narrows the rows on
// screen without changing any selection. Everything offered starts selected
// and the user deselects what should not be downloaded.
type pickModel struct {
	// all and allSel are the complete selection state: the videos left by
	// the range filter, in playlist order, and their selection flags.
	// videos, selected and visible are the projection currently on screen:
	// visible maps a visible row back to its index in all, so toggling a
	// row under a search filter changes the very same video the unfiltered
	// list shows - the filter never changes what gets downloaded.
	all      []ytdlp.Video
	allSel   []bool
	videos   []ytdlp.Video // filtered subset, absolute Index preserved
	selected []bool        // parallel to videos; true = queued for download
	visible  []int         // parallel to videos; index into all
	cursor   int           // index into videos of the highlighted row

	vp viewport.Model

	// search is the inline '/' filter: a case-insensitive substring match
	// over titles and ids. searching is true while the field owns the
	// keyboard.
	search    textinput.Model
	searching bool
	query     string

	hint   string // transient message, rendered in place of the ready counter
	width  int
	height int

	// Range filter that produced videos, kept for the header label.
	filtered   bool
	rangeStart int
	rangeEnd   int

	// rows caches the rendered list rows (parallel to videos) so Update can
	// patch only the rows a key changed instead of rebuilding every row each
	// frame. lastRowW is the content width the cache was laid out for; 0 means
	// the cache is stale (or never built) and View must rebuild it. The width
	// is always the clamped content width (contentWidth of the terminal
	// width), which is the same basis View lays the screen out for, so the
	// cache warmed by the window-size message always matches the frame.
	rows     []string
	lastRowW int

	// rowsGen/vpGen keep the viewport content in sync with the row cache.
	// rowsGen is bumped whenever rows is replaced by a fresh slice; vpGen
	// records the generation the viewport was last handed. bubbles'
	// SetContentLines rescans every line (newline check plus longest-line
	// measurement), which costs 0.4-0.6 ms at 200 rows, so it may only run
	// when the rows actually changed. In-place patches (patchRows) need no
	// rescan at all: the viewport keeps the slice it was given, so it shares
	// the backing array with m.rows and sees patched strings directly. The
	// pair lives in the model (not as a flag cleared in View) because View
	// runs on a copy: a flag cleared there would never reach the stored model.
	rowsGen uint64
	vpGen   uint64
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
		search:     newPickSearch(),
	}

	if pl != nil {
		for _, v := range pl.Videos {
			if rangeStart > 0 && v.Index < rangeStart {
				continue
			}
			if rangeEnd > 0 && v.Index > rangeEnd {
				continue
			}
			m.all = append(m.all, v)
		}
	}

	m.allSel = make([]bool, len(m.all))
	for i := range m.allSel {
		m.allSel[i] = true
	}
	return m.applyFilter()
}

// newPickSearch builds the inline search field with no styling of its own:
// the styles around it come from the palette.
func newPickSearch() textinput.Model {
	in := textinput.New()
	in.Prompt = "/"
	in.Placeholder = "filter title or id"
	in.CharLimit = 128
	return in
}

// Init implements the Bubble Tea model interface; the screen has no startup
// work of its own.
func (m pickModel) Init() tea.Cmd {
	return nil
}

// Update handles window size messages, the selection keys, the search field
// and mouse-wheel scrolling. Every other message is ignored because
// navigation beyond this screen is owned by the app.
func (m pickModel) Update(msg tea.Msg) (pickModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Record the terminal size and warm the row cache for the layout
		// width. Returning the rebuilt model keeps rows and lastRowW, so
		// patchRows on later keys patches single rows instead of View
		// rebuilding everything each frame. View still owns the viewport
		// sizing and the cold-start fallback rebuild.
		//
		// The cache is warmed at the clamped content width - the very width
		// View lays the screen out for - never at the raw terminal width: on
		// a terminal wider than maxContentWidth (or narrower than
		// minBoxColumns) a raw-width cache would miss on every frame and
		// force an O(N) rebuild each time.
		m.width, m.height = msg.Width, msg.Height
		w := contentWidth(msg.Width)
		if len(m.videos) > 0 && w > 0 {
			m = m.rebuildRows(w)
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateKeys(msg)

	case tea.MouseWheelMsg:
		// The wheel moves the cursor, which keeps the highlighted row and
		// the scroll position in step; the other screens ignore mouse
		// events and the app forwards them here unmodified.
		switch msg.Button {
		case tea.MouseWheelUp:
			return m.moveCursor(-pickMouseScrollLines)
		case tea.MouseWheelDown:
			return m.moveCursor(pickMouseScrollLines)
		}
		return m, nil
	}
	return m, nil
}

// updateKeys handles the selection, movement, jump and search keys. Space also
// clears the transient hint.
func (m pickModel) updateKeys(msg tea.KeyPressMsg) (pickModel, tea.Cmd) {
	keyStr := msg.String()
	oldCursor := m.cursor

	switch keyStr {
	case "up", "k":
		m.cursor--
	case "down", "j":
		m.cursor++
	case "space":
		m = m.toggleCursor()
		m.hint = ""
	case "a", "A":
		m = m.setAll(true)
	case "n", "N":
		m = m.setAll(false)
	case "/":
		return m.beginSearch()
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.videos) - 1
	case "pgup":
		m.cursor -= max(m.vp.Height(), 1)
	case "pgdown":
		m.cursor += max(m.vp.Height(), 1)
	}

	m = m.clampCursor(m.cursor)

	// Refresh only the cached rows this key affected. Before the first
	// rebuild (lastRowW == 0) there is nothing to patch: View rebuilds the
	// whole cache from scratch on the next frame.
	switch keyStr {
	case "up", "k", "down", "j", "home", "g", "end", "G", "pgup", "pgdown":
		m = m.patchRows(oldCursor, m.cursor)
	case "space":
		m = m.patchRows(m.cursor)
	case "a", "A", "n", "N":
		if m.lastRowW > 0 {
			m = m.rebuildRows(m.lastRowW)
		}
	}
	return m, nil
}

// beginSearch focuses the inline filter. The current query stays in the field
// so '/' edits an active filter instead of discarding it.
func (m pickModel) beginSearch() (pickModel, tea.Cmd) {
	m.searching = true
	m.hint = ""
	m.search.SetWidth(min(max(m.width-12, pickSearchMinWidth), pickSearchMaxWidth))
	return m, m.search.Focus()
}

// updateSearch feeds every key to the focused search field, except the two
// that leave search mode: esc clears the filter and restores the full list,
// enter keeps the filter and hands the keyboard back to the list.
func (m pickModel) updateSearch(msg tea.KeyPressMsg) (pickModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.search.Blur()
		m.search.SetValue("")
		return m.setQuery(""), nil
	case "enter":
		m.searching = false
		m.search.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return m.setQuery(m.search.Value()), cmd
}

// setQuery applies a new filter and rebuilds the visible projection. The
// cursor stays at the same row position, clamped to the new row count.
func (m pickModel) setQuery(q string) pickModel {
	q = strings.TrimSpace(q)
	if q == m.query {
		return m
	}
	m.query = q
	return m.applyFilter()
}

// Retheme drops the cached rows so the next frame rebuilds them with the
// active palette: each cached row embeds the styles it was rendered with, so
// a theme flip would otherwise leave the list in the old colors until a key
// happened to patch those rows. The cache is warmed again immediately when
// the layout width is known, exactly like applyFilter does after a query.
func (m pickModel) Retheme() pickModel {
	width := m.lastRowW
	m.rows = nil
	m.lastRowW = 0
	m.rowsGen++
	if width > 0 && len(m.videos) > 0 {
		m = m.rebuildRows(width)
	}
	return m
}

// applyFilter rebuilds videos/selected/visible from all/allSel for the active
// query. It drops the row cache and warms it again when a layout width is
// already known, so the cache always describes the rows on screen.
func (m pickModel) applyFilter() pickModel {
	needle := strings.ToLower(strings.TrimSpace(m.query))

	videos := make([]ytdlp.Video, 0, len(m.all))
	selected := make([]bool, 0, len(m.all))
	visible := make([]int, 0, len(m.all))
	for i, v := range m.all {
		if needle != "" && !videoMatches(v, needle) {
			continue
		}
		videos = append(videos, v)
		selected = append(selected, m.allSel[i])
		visible = append(visible, i)
	}
	m.videos, m.selected, m.visible = videos, selected, visible

	width := m.lastRowW
	m.rows = nil
	m.lastRowW = 0
	m.rowsGen++
	if width > 0 && len(m.videos) > 0 {
		m = m.rebuildRows(width)
	}
	return m.clampCursor(m.cursor)
}

// videoMatches reports whether v matches the lower-case needle in its title
// or its id.
func videoMatches(v ytdlp.Video, needle string) bool {
	return strings.Contains(strings.ToLower(v.Title), needle) ||
		strings.Contains(strings.ToLower(v.ID), needle)
}

// moveCursor shifts the cursor by delta and patches the two affected rows.
func (m pickModel) moveCursor(delta int) (pickModel, tea.Cmd) {
	old := m.cursor
	m = m.clampCursor(m.cursor + delta)
	m = m.patchRows(old, m.cursor)
	return m, nil
}

// clampCursor keeps the cursor inside the visible row range.
func (m pickModel) clampCursor(cursor int) pickModel {
	if len(m.videos) == 0 {
		m.cursor = 0
		return m
	}
	m.cursor = min(max(cursor, 0), len(m.videos)-1)
	return m
}

// toggleCursor flips the selection of the highlighted row.
func (m pickModel) toggleCursor() pickModel {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return m
	}
	idx := m.visible[m.cursor]
	if idx < 0 || idx >= len(m.allSel) {
		return m
	}
	m.allSel[idx] = !m.allSel[idx]
	if m.cursor < len(m.selected) {
		m.selected[m.cursor] = m.allSel[idx]
	}
	return m
}

// setAll selects or clears every offered video, visible or filtered out.
func (m pickModel) setAll(selected bool) pickModel {
	for i := range m.allSel {
		m.allSel[i] = selected
	}
	for i := range m.selected {
		m.selected[i] = selected
	}
	return m
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
// touched every row (select all / select none). The viewport is re-synced
// right away, so a rebuild that happens on a key (a/n, window size) leaves
// View nothing to scan.
func (m pickModel) rebuildRows(width int) pickModel {
	rows := make([]string, len(m.videos))
	for i := range m.videos {
		rows[i] = m.renderRow(i, width)
	}
	m.rows = rows
	m.lastRowW = width
	m.rowsGen++
	return m.syncRows()
}

// syncRows hands the current row cache to the viewport when the viewport does
// not already hold that generation. It is the only place SetContentLines runs:
// patches mutate the cached rows in place (and the viewport shares their
// backing array), so they never need a rescan.
func (m pickModel) syncRows() pickModel {
	if m.vpGen == m.rowsGen {
		return m
	}
	m.vp.SetContentLines(m.rows)
	m.vpGen = m.rowsGen
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

// View renders the header, the optional search bar, the scrollable list of
// videos, the hint line and the filter hint. width and height are the content
// dimensions; the viewport takes the remaining rows. An empty list renders a
// muted placeholder instead of the viewport.
func (m pickModel) View(width, height int) string {
	m.width, m.height = width, height

	total := len(m.videos)
	count := m.SelectedCount()

	header := StyleTitle.Render(fmt.Sprintf("Videos - %d selected of %d", count, total))
	if m.filtered {
		header += StyleMuted.Render(m.rangeLabel())
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")

	searchLine := m.searchLine()
	used := 2 // header + hint line
	if searchLine != "" {
		b.WriteString(searchLine)
		b.WriteString("\n")
		used++
	}

	if total == 0 {
		message := "(no videos)"
		if len(m.all) > 0 {
			message = "(no video matches the filter)"
		}
		b.WriteString(StyleMuted.Render(message))
		b.WriteString("\n")
		b.WriteString(m.hintLine(count))
		return b.String()
	}

	// The row cache is rebuilt only when it is cold or was laid out for a
	// different content width; Update patches the affected rows on keys. The
	// viewport content is then synchronized by generation: rebuilding hands
	// it the fresh slice, patching needs nothing because the viewport shares
	// the cached rows' backing array, and a frame whose cache is already in
	// place skips bubbles' O(N) line rescan entirely.
	if m.rows == nil || m.lastRowW != width {
		m = m.rebuildRows(width)
	} else {
		m = m.syncRows()
	}

	h := max(3, height-used)
	w := max(20, width-2)
	if m.vp.Height() != h {
		m.vp.SetHeight(h)
	}
	if m.vp.Width() != w {
		m.vp.SetWidth(w)
	}

	// Content is already in place (see above), so the offset logic below
	// clamps against the lines the viewport actually holds. View works on a
	// receiver copy, which is why the content has to be synced by rebuildRows
	// (on the model Update returns) rather than here.

	// Keep the cursor row inside the viewport window.
	off := m.vp.YOffset()
	vis := m.vp.Height()
	if m.cursor < off {
		m.vp.SetYOffset(m.cursor)
	} else if m.cursor >= off+vis {
		m.vp.SetYOffset(m.cursor - vis + 1)
	}

	b.WriteString(m.vp.View())
	b.WriteString("\n")
	b.WriteString(m.hintLine(count))
	return b.String()
}

// searchLine renders the inline filter bar while it is focused, or a reminder
// of the active filter once search mode is left. It is empty when no filter
// is in play, which gives the list one more row.
func (m pickModel) searchLine() string {
	switch {
	case m.searching:
		m.search.SetWidth(min(max(m.width-12, pickSearchMinWidth), pickSearchMaxWidth))
		return StyleAccent.Render("search ") + styledInput(m.search).View()
	case m.query != "":
		return StyleMuted.Render("filter ") + StyleValue.Render(Truncate(m.query, 30)) +
			StyleMuted.Render("  (/ to edit, esc in search clears)")
	default:
		return ""
	}
}

// hintLine renders the transient hint when one is set, and the selection
// counter otherwise. count is the selected total View already computed, so the
// O(N) scan behind SelectedCount runs once per frame instead of twice (the
// header and the footer both carry the count).
func (m pickModel) hintLine(count int) string {
	if m.hint != "" {
		return StyleError.Render(m.hint)
	}
	line := StyleMuted.Render(fmt.Sprintf("%d of %d selected", count, len(m.all)))
	if n := len(m.videos); n != len(m.all) {
		line += StyleMuted.Render(fmt.Sprintf("  filtered: %d shown", n))
	}
	return line
}

// Selected returns a copy of every still-selected video, in playlist order.
// A search filter never changes the result: it only narrows the rows on
// screen.
func (m pickModel) Selected() []ytdlp.Video {
	out := make([]ytdlp.Video, 0, len(m.all))
	for i, v := range m.all {
		if i < len(m.allSel) && m.allSel[i] {
			out = append(out, v)
		}
	}
	return out
}

// SelectedCount reports how many videos are currently selected.
func (m pickModel) SelectedCount() int {
	n := 0
	for i := range m.allSel {
		if m.allSel[i] {
			n++
		}
	}
	return n
}

// TotalShown reports how many videos the playlist offered after the range
// filter, regardless of the search filter.
func (m pickModel) TotalShown() int {
	return len(m.all)
}

// Searching reports whether the inline search field owns the keyboard. While
// it does, the app forwards every key to this screen without interpreting it.
func (m pickModel) Searching() bool {
	return m.searching
}

// SetHint stores a transient message shown in place of the ready counter.
func (m pickModel) SetHint(s string) pickModel {
	m.hint = s
	return m
}

// rangeLabel renders the header suffix for the active range filter, e.g.
// " (range 3-40)", " (range 3-)" or " (range -40)".
func (m pickModel) rangeLabel() string {
	switch {
	case m.rangeStart > 0 && m.rangeEnd > 0:
		return fmt.Sprintf(" (range %d-%d)", m.rangeStart, m.rangeEnd)
	case m.rangeStart > 0:
		return fmt.Sprintf(" (range %d-)", m.rangeStart)
	case m.rangeEnd > 0:
		return fmt.Sprintf(" (range -%d)", m.rangeEnd)
	default:
		return ""
	}
}
