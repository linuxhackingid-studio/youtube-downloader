// Tests for the T7 redesign: the frame's geometry guarantees (never wider
// than the terminal, centered on wide terminals, clipped on short ones), the
// graceful shutdown that cancels the jobs manager, the pick screen's search
// and mouse-wheel additions, and the download screen's frame memoization.

package tui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"linuxhackingid.com/youtube-downloader/internal/config"
	"linuxhackingid.com/youtube-downloader/internal/jobs"
	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// testAppModel builds a fully populated root model for rendering tests: every
// screen model is constructed, so a frame can be rendered for each screen
// without any process, terminal or network.
func testAppModel(t *testing.T, scr screen) appModel {
	t.Helper()

	cfg := config.Default()
	m := appModel{
		screen:  scr,
		cfg:     cfg,
		check:   newCheckModel(),
		form:    newFormModel(cfg),
		loading: newLoadingModel("Fetching playlist..."),
		pick: newPickModel(&ytdlp.Playlist{Videos: []ytdlp.Video{
			{Index: 1, ID: "a", Title: "First Video", Duration: 65},
			{Index: 2, ID: "b", Title: "Second Video", Duration: 125},
			{Index: 3, ID: "c", Title: "Third Video"},
		}}, 0, 0),
		done: newDoneModel(cfg, nil, 0),
	}
	m.dl = newDownloadModel(nil, cfg, 3)
	return m
}

// TestFrameNeverExceedsTerminalWidth is the narrow-terminal contract: no line
// of any screen may be wider than the terminal, from 40 columns up.
func TestFrameNeverExceedsTerminalWidth(t *testing.T) {
	screens := []screen{scrCheck, scrForm, scrLoading, scrPick, scrDownload, scrDone}

	for _, width := range []int{20, 40, 60, 80, 100, 120, 200} {
		for _, scr := range screens {
			m := testAppModel(t, scr)
			m.width, m.height = width, 40

			out := m.View().Content
			for i, line := range strings.Split(out, "\n") {
				if got := lg.Width(line); got > width {
					t.Errorf("screen %v, terminal %d: line %d width = %d, want <= %d (%q)",
						scr, width, i, got, width, line)
				}
			}
		}
	}
}

// TestFrameClipsShortTerminal checks the height contract: a body taller than
// the terminal is cut off rather than left to wrap the layout apart.
func TestFrameClipsShortTerminal(t *testing.T) {
	m := testAppModel(t, scrForm)
	m.width, m.height = 80, 8

	out := m.View().Content
	if got, want := len(strings.Split(out, "\n")), 8; got > want {
		t.Errorf("frame rendered %d lines for a %d-row terminal, want at most %d", got, want, want)
	}
	if !strings.Contains(out, "hidden") {
		t.Errorf("frame = %q, want the truncation marker when lines were dropped", out)
	}
}

// TestFrameCentersWideTerminal pins the wide-terminal promise: the content
// block is centered instead of hugging the left edge.
func TestFrameCentersWideTerminal(t *testing.T) {
	m := testAppModel(t, scrCheck)
	m.width, m.height = 200, 40

	out := m.View().Content
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		t.Fatal("frame is empty")
	}
	first := lines[0]
	left := len(first) - len(strings.TrimLeft(first, " "))
	if left == 0 {
		t.Errorf("frame line = %q, want leading padding on a 200-column terminal", first)
	}
	if got := lg.Width(first); got != 200 {
		t.Errorf("centered line width = %d, want the full terminal width 200", got)
	}
}

// TestViewReportsWindowTitleAndMouseMode pins the terminal integration: the
// alternate screen, cell-motion mouse reporting and the window title.
func TestViewReportsWindowTitleAndMouseMode(t *testing.T) {
	m := testAppModel(t, scrCheck)
	m.width, m.height = 100, 40

	v := m.View()
	if !v.AltScreen {
		t.Error("View().AltScreen = false, want the alternate screen")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("View().MouseMode = %v, want tea.MouseModeCellMotion", v.MouseMode)
	}
	if v.WindowTitle != appName {
		t.Errorf("View().WindowTitle = %q, want %q", v.WindowTitle, appName)
	}

	m.screen = scrDownload
	if title := m.View().WindowTitle; !strings.HasPrefix(title, appName+" - ") {
		t.Errorf("download window title = %q, want it to start with %q", title, appName+" - ")
	}
}

// TestShutdownCancelsManager pins the graceful-shutdown contract: quitting
// cancels every child process through the jobs manager before the program
// exits. Start is never called here, so no worker goroutine or process is
// spawned; the test passes because quit() cancels the manager, which closes
// its Done channel and marks the queued jobs canceled.
func TestShutdownCancelsManager(t *testing.T) {
	mgr := jobs.NewManager(nil, jobs.Options{Videos: []ytdlp.Video{{ID: "a"}, {ID: "b"}}})

	m := appModel{mgr: mgr}
	model, cmd := m.quit()
	if cmd == nil {
		t.Fatal("quit() returned no command, want tea.Quit")
	}

	app, ok := model.(appModel)
	if !ok {
		t.Fatalf("quit() returned %T, want appModel", model)
	}
	if app.mgr == nil {
		t.Fatal("quit() dropped the jobs manager")
	}

	select {
	case <-mgr.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("manager still running after quit(): children would outlive the TUI")
	}
	if s := mgr.Summary(); s.Canceled == 0 && s.Done == 0 {
		t.Errorf("summary = %+v, want the jobs to be canceled or finished", s)
	}
}

// TestShutdownWithoutManager pins the nil-manager path every screen before
// the first download takes.
func TestShutdownWithoutManager(t *testing.T) {
	m := appModel{}
	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit() without a manager returned no command, want tea.Quit")
	}
}

// TestPickSearchFiltersRows pins the new search mode: '/' focuses the field,
// typed runes narrow the visible rows without touching the selection, and esc
// clears the filter and leaves search mode.
func TestPickSearchFiltersRows(t *testing.T) {
	m := newPickTestModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.Update(pickKeyRune('/'))
	if !m.Searching() {
		t.Fatal("Searching() = false after '/', want true")
	}

	for _, r := range "two" {
		m, _ = m.Update(pickKeyRune(r))
	}
	if got := len(m.videos); got != 1 {
		t.Fatalf("visible rows = %d after filtering %q, want 1", got, "two")
	}
	if m.videos[0].Title != "Two" {
		t.Errorf("visible row = %q, want %q", m.videos[0].Title, "Two")
	}
	if got := m.SelectedCount(); got != 3 {
		t.Errorf("SelectedCount() = %d with a filter active, want 3 (the filter never changes the selection)", got)
	}
	if len(m.rows) != 1 {
		t.Errorf("row cache holds %d rows, want 1 for the filtered list", len(m.rows))
	}

	// Enter keeps the filter and hands the keyboard back to the list; space
	// then toggles the filtered row and maps back to the same video.
	m, _ = m.Update(e2eEnter())
	if m.Searching() {
		t.Error("Searching() = true after enter, want the filter kept but search mode left")
	}
	if got := len(m.videos); got != 1 {
		t.Errorf("visible rows = %d after enter, want the filter kept (1 row)", got)
	}

	m, _ = m.Update(pickKeySpace())
	if m.allSel[1] {
		t.Error("allSel[1] = true after toggling the filtered row, want false")
	}
	if got := m.SelectedCount(); got != 2 {
		t.Errorf("SelectedCount() = %d after toggling one video, want 2", got)
	}

	// '/' again edits the active filter, and esc clears it and restores the
	// full list.
	m, _ = m.Update(pickKeyRune('/'))
	if !m.Searching() {
		t.Error("Searching() = false after '/', want true")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Searching() {
		t.Error("Searching() = true after esc, want false")
	}
	if m.query != "" {
		t.Errorf("query = %q after esc, want it cleared", m.query)
	}
	if got := len(m.videos); got != 3 {
		t.Errorf("visible rows = %d after esc, want the full list back", got)
	}
}

// TestPickJumpKeys pins home/end and g/G.
func TestPickJumpKeys(t *testing.T) {
	m := newPickTestModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.Update(pickKeyRune('G'))
	if m.cursor != 2 {
		t.Errorf("cursor = %d after G, want 2 (last row)", m.cursor)
	}
	if !strings.Contains(m.rows[2], ">") || strings.Contains(m.rows[0], ">") {
		t.Errorf("rows = %q, want the cursor marker on the last row only", m.rows)
	}

	m, _ = m.Update(pickKeyRune('g'))
	if m.cursor != 0 {
		t.Errorf("cursor = %d after g, want 0 (first row)", m.cursor)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.cursor != 2 {
		t.Errorf("cursor = %d after end, want 2", m.cursor)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if m.cursor != 0 {
		t.Errorf("cursor = %d after home, want 0", m.cursor)
	}
}

// TestPickMouseWheelScrolls pins wheel scrolling on the pick screen.
func TestPickMouseWheelScrolls(t *testing.T) {
	m := newPickTestModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.cursor != 2 {
		t.Errorf("cursor = %d after one wheel-down notch, want 2 (clamped to the last row)", m.cursor)
	}
	m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.cursor != 0 {
		t.Errorf("cursor = %d after one wheel-up notch, want 0 (clamped to the first row)", m.cursor)
	}
}

// TestDownloadViewCacheReusesFrame pins the memoization: two renders with no
// state change hand back the identical string, and a revision bump rebuilds
// it.
func TestDownloadViewCacheReusesFrame(t *testing.T) {
	mgr := jobs.NewManager(nil, jobs.Options{})
	m := newDownloadModel(mgr, config.Default(), 0)

	first := m.View(80, 24)
	if second := m.View(80, 24); second != first {
		t.Error("second render built a new frame, want the memoized one")
	}
	if !m.cache.valid {
		t.Fatal("cache.valid = false after a render, want it filled")
	}
	if m.cache.rev != mgr.Revision() {
		t.Errorf("cache revision = %d, want %d", m.cache.rev, mgr.Revision())
	}

	m.Invalidate()
	if m.cache.valid {
		t.Error("cache.valid = true after Invalidate, want false")
	}
}

// TestContentWidthClamps pins the content width contract: never above the
// maximum, never above the terminal, never below the box floor.
func TestContentWidthClamps(t *testing.T) {
	tests := []struct {
		term, want int
	}{
		{200, maxContentWidth},
		{120, maxContentWidth},
		{100, 100},
		{80, 80},
		{40, 40},
		{0, defaultViewWidth},
		{10, minBoxColumns},
	}
	for _, tt := range tests {
		if got := contentWidth(tt.term); got != tt.want {
			t.Errorf("contentWidth(%d) = %d, want %d", tt.term, got, tt.want)
		}
	}
}

// TestClipStyledWidthAndEscapes pins the clipping helper: display width never
// exceeds the budget, the ellipsis marks the cut, and the escape sequences it
// kept are those it copied verbatim.
func TestClipStyledWidthAndEscapes(t *testing.T) {
	styled := "\x1b[31m" + strings.Repeat("x", 40) + "\x1b[0m"

	out := clipStyled(styled, 10)
	if got := lg.Width(out); got > 10 {
		t.Errorf("clipStyled(40 cells, 10) width = %d, want <= 10", got)
	}
	if !strings.Contains(out, "…") {
		t.Error("clipStyled did not mark the truncation")
	}
	if !strings.HasPrefix(out, "\x1b[31m") {
		t.Error("clipStyled dropped the leading color sequence")
	}
	if !strings.HasSuffix(out, "\x1b[0m") {
		t.Error("clipStyled left the color sequence open")
	}

	if got := clipStyled("short", 10); got != "short" {
		t.Errorf("clipStyled(short, 10) = %q, want it unchanged", got)
	}
}

// TestDownloadViewAndCancelRunConcurrently is a smoke test for the one piece
// of shared state the redesign adds: the download screen renders on the
// Bubble Tea event goroutine while Cancel (which kills child processes) runs
// from a command goroutine. The manager guards its own state with a mutex and
// the view cache is only touched while rendering, so the pattern must neither
// deadlock nor panic. Run it with -race where a race-enabled toolchain is
// available.
func TestDownloadViewAndCancelRunConcurrently(t *testing.T) {
	mgr := jobs.NewManager(nil, jobs.Options{Videos: []ytdlp.Video{{ID: "a"}, {ID: "b"}}})
	mgr.Start(context.Background())

	m := newDownloadModel(mgr, config.Default(), 2)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if view := m.View(80, 24); !strings.Contains(view, "Downloading") {
				t.Errorf("View() = %q, want it to carry the screen title", view)
				return
			}
			_ = m.Title()
		}
	}()
	go func() {
		defer wg.Done()
		mgr.Cancel()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent View/Cancel did not finish in 5s, want no deadlock")
	}
}

// TestThemeFlipRewiresScreens pins the dark/light wiring end to end: the
// terminal's background report rebuilds the renderers that cache what they
// drew - the pick screen's rows and the download screen's memoized frame -
// and reports the new palette through the shared theme state.
func TestThemeFlipRewiresScreens(t *testing.T) {
	defer ApplyTheme(true)

	m := testAppModel(t, scrCheck)
	m.width, m.height = 100, 40
	m.dl = newDownloadModel(jobs.NewManager(nil, jobs.Options{}), config.Default(), 0)

	// Warm both caches the way a first frame would.
	m.pick, _ = m.pick.Update(tea.WindowSizeMsg{Width: contentWidth(m.width), Height: 30})
	darkRows := append([]string(nil), m.pick.rows...)
	m.dl.View(contentWidth(m.width), 30)

	updated, _ := m.Update(tea.BackgroundColorMsg{Color: color.White})
	got, ok := updated.(appModel)
	if !ok {
		t.Fatalf("Update returned %T, want appModel", updated)
	}

	if ThemeIsDark() {
		t.Fatal("ThemeIsDark() = true after the terminal reported a light background, want the light palette")
	}
	if len(got.pick.rows) != len(darkRows) {
		t.Fatalf("pick rows = %d after the flip, want %d", len(got.pick.rows), len(darkRows))
	}
	if got.pick.rows[0] == darkRows[0] {
		t.Error("pick row kept the dark palette after the theme flip, want it rebuilt")
	}
	if got.dl.cache.valid {
		t.Error("download view cache stayed valid after the theme flip, want it invalidated")
	}
}

// TestPickRethemeRebuildsRows pins that a theme flip reaches the pick list:
// the cached rows embed the styles they were rendered with, so Retheme has to
// drop and rebuild them or the list would keep the previous palette until a
// key happened to patch those rows.
func TestPickRethemeRebuildsRows(t *testing.T) {
	defer ApplyTheme(true)

	m := newPickTestModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	before := append([]string(nil), m.rows...)

	ApplyTheme(false)
	m = m.Retheme()

	if m.lastRowW != 100 {
		t.Errorf("lastRowW = %d after Retheme, want the width warmed again (100)", m.lastRowW)
	}
	if len(m.rows) != 3 {
		t.Fatalf("len(rows) = %d after Retheme, want 3", len(m.rows))
	}
	if m.rows[0] == before[0] {
		t.Errorf("rows[0] = %q after Retheme, want it rebuilt with the new palette", m.rows[0])
	}
	if !strings.Contains(m.rows[1], "[x]") {
		t.Errorf("rows[1] = %q after Retheme, want the selection state preserved", m.rows[1])
	}
}

// TestDownloadRowFillsWidth pins the row layout: on a wide terminal the title
// gets every column the detail section does not need (instead of a fixed
// budget that leaves a dead gap), the row still ends at the content width,
// and a narrow terminal falls back to the compact detail form rather than
// clipping mid-detail.
func TestDownloadRowFillsWidth(t *testing.T) {
	const title = "A title long enough that it cannot possibly fit on one row next to the detail section"

	mgr := jobs.NewManager(nil, jobs.Options{})
	m := newDownloadModel(mgr, config.Default(), 1)
	m.snapshot = []jobs.JobSnapshot{{
		Video:      ytdlp.Video{Index: 2, Title: title},
		Status:     jobs.StatusRunning,
		Percent:    42,
		SpeedBps:   2.5 * 1024 * 1024,
		ETASeconds: 62,
	}}

	wide := m.jobRow(m.snapshot[0], 100)
	if got := lg.Width(wide); got != 100 {
		t.Errorf("wide row width = %d, want the content width 100 (row = %q)", got, wide)
	}
	// The old fixed budget cut the title after 41 columns; the row must now
	// carry a visibly longer slice of it.
	if !strings.Contains(plainText(wide), title[:50]) {
		t.Errorf("wide row = %q, want more of the title than the fixed 42-column budget", wide)
	}
	if !strings.Contains(wide, "ETA 1m 02s") {
		t.Errorf("wide row = %q, want the full detail section", wide)
	}

	narrow := m.jobRow(m.snapshot[0], 40)
	if got := lg.Width(narrow); got > 40 {
		t.Errorf("narrow row width = %d, want <= 40", got)
	}
	if !strings.Contains(narrow, "42%") {
		t.Errorf("narrow row = %q, want the compact percentage", narrow)
	}
	if strings.Contains(narrow, "MiB/s") {
		t.Errorf("narrow row = %q, want the compact detail form without the speed", narrow)
	}
}

// TestSkippedRowIsNotFullPercent pins the skipped marker: a job that was
// already on disk did not download anything, so it must not read as 100%.
func TestSkippedRowIsNotFullPercent(t *testing.T) {
	mgr := jobs.NewManager(nil, jobs.Options{})
	m := newDownloadModel(mgr, config.Default(), 1)
	row := m.jobRow(jobs.JobSnapshot{
		Video:  ytdlp.Video{Index: 6, Title: "Sixth Video"},
		Status: jobs.StatusSkipped,
	}, 100)

	if strings.Contains(plainText(row), "100%") {
		t.Errorf("skipped row = %q, want it not to claim a completed download", row)
	}
	if !strings.Contains(row, "already on disk") {
		t.Errorf("skipped row = %q, want the already-on-disk marker", row)
	}
}

// TestFormCompactRowsFitWidth pins the compact form layout: every row fits the
// content width, and the text inputs are sized so nothing is clipped over
// their blank padding (which used to draw a stray ellipsis at the margin).
func TestFormCompactRowsFitWidth(t *testing.T) {
	const width = 80

	form := newFormModel(config.Default())
	form.inputs[formSlotURL].SetValue("https://www.youtube.com/playlist?list=PLtest")

	// A body height below formCompactMinHeight selects the compact layout.
	body := form.View(width, formCompactMinHeight-1)
	for i, line := range strings.Split(body, "\n") {
		if got := lg.Width(line); got > width {
			t.Errorf("compact row %d width = %d, want <= %d (%q)", i, got, width, plainText(line))
		}
		if strings.Contains(plainText(line), "…") {
			t.Errorf("compact row %d = %q, want no clipped row", i, plainText(line))
		}
	}
}

// compile-time pin: cmd/ytd ends the program by asserting the final model
// implements this interface, so the method set must keep Shutdown exported.
var _ interface{ Shutdown() } = appModel{}

// TestDownloadLogPanelStripsTerminalEscapes is the regression test for the
// log-panel escape injection: yt-dlp's stdout/stderr is untrusted, and a short
// line carrying an OSC or CSI sequence measures as a handful of visible columns
// (lg.Width counts escapes as zero), so the frame's truncate-only treatment
// would pass it to the terminal verbatim. Every log line must be sanitized
// before it is handed to Panel.
func TestDownloadLogPanelStripsTerminalEscapes(t *testing.T) {
	// OSC 52 - "set the clipboard" - is the cheapest way to demonstrate the
	// bug: it fits in a few columns and needs no parameter bytes.
	const osc = "\x1b]52;c;AAAA\x07"

	mgr := jobs.NewManager(nil, jobs.Options{})
	m := testAppModel(t, scrDownload)
	m.width, m.height = 100, 40
	m.dl = newDownloadModel(mgr, m.cfg, 1)
	m.dl.snapshot = []jobs.JobSnapshot{{
		Video:  ytdlp.Video{Index: 1, Title: "First Video"},
		Status: jobs.StatusRunning,
	}}
	m.dl.logs = []string{
		"[download] " + osc + "clipboard payload",
		"[download] 100% of 1.00MiB",
	}

	out := m.View().Content
	if strings.Contains(out, "\x1b]") {
		t.Errorf("download frame carries an OSC introducer, want every log line sanitized:\n%q", out)
	}
	if strings.ContainsRune(out, 0x07) {
		t.Errorf("download frame carries a raw BEL, want every log line sanitized:\n%q", out)
	}
	// The line must survive as text, not vanish: sanitizing strips the
	// sequence, not the log entry.
	if !strings.Contains(out, "clipboard payload") {
		t.Errorf("download frame dropped the sanitized log line, want %q present:\n%q", "clipboard payload", out)
	}
	if !strings.Contains(out, "100% of 1.00MiB") {
		t.Errorf("download frame dropped a clean log line:\n%q", out)
	}
}

// TestPickCacheWarmsAtContentWidth pins the width basis of the pick row cache:
// the window-size message warms the cache at the clamped content width - the
// one View lays the screen out for - so a terminal wider than maxContentWidth
// (or narrower than the box floor) does not miss the cache on every frame and
// rebuild every row. The viewport must also already hold that content, so the
// frame never re-scans the rows.
func TestPickCacheWarmsAtContentWidth(t *testing.T) {
	tests := []struct {
		term, want int
	}{
		{200, maxContentWidth},
		{120, maxContentWidth},
		{80, 80},
		{10, minBoxColumns},
	}

	for _, tt := range tests {
		m := newPickTestModel()
		m, _ = m.Update(tea.WindowSizeMsg{Width: tt.term, Height: 40})

		if m.lastRowW != tt.want {
			t.Errorf("terminal %d: lastRowW = %d, want the clamped content width %d",
				tt.term, m.lastRowW, tt.want)
		}
		if len(m.rows) != 3 {
			t.Fatalf("terminal %d: len(rows) = %d, want 3", tt.term, len(m.rows))
		}
		if m.vpGen != m.rowsGen {
			t.Errorf("terminal %d: viewport generation = %d, want the row generation %d (content synced)",
				tt.term, m.vpGen, m.rowsGen)
		}

		// The frame renders the warmed cache, and a patching key does not
		// invalidate it again. The assertion targets tokens that survive the
		// narrowest layout (the row titles are truncated there, the marker and
		// the header are not).
		if out := m.View(tt.want, 20); !strings.Contains(out, "Videos - ") || !strings.Contains(out, "[x]") {
			t.Errorf("terminal %d: frame = %q, want the warmed rows rendered", tt.term, out)
		}
		m, _ = m.Update(pickKeyDown())
		if m.cursor != 1 || m.vpGen != m.rowsGen {
			t.Errorf("terminal %d: after down cursor = %d, viewport generation = %d, want 1 and %d",
				tt.term, m.cursor, m.vpGen, m.rowsGen)
		}
	}
}

// TestShutdownCancelsPlaylistFetch pins the fetch side of shutdown: a quit
// while the playlist fetch is loading must cancel its context, or the yt-dlp
// child would outlive the TUI.
func TestShutdownCancelsPlaylistFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	m := appModel{fetchCancel: cancel}
	model, _ := m.quit()
	app, ok := model.(appModel)
	if !ok {
		t.Fatalf("quit() returned %T, want appModel", model)
	}
	select {
	case <-ctx.Done():
	default:
		t.Error("quit() did not cancel the in-flight playlist fetch")
	}
	if app.fetchCancel != nil {
		t.Error("quit() kept the canceled fetch context, want it released")
	}
}

// TestShutdownIsSafeOnZeroModelAndTwice pins the cmd/ytd contract: the exported
// Shutdown is called on the final model even when the program never left the
// setup screen, and both the in-app exit path and the final-model cleanup can
// reach it, so a zero model and a double call must both be safe.
func TestShutdownIsSafeOnZeroModelAndTwice(t *testing.T) {
	var zero appModel
	zero.Shutdown()

	mgr := jobs.NewManager(nil, jobs.Options{Videos: []ytdlp.Video{{ID: "a"}}})
	m := appModel{mgr: mgr}
	m.Shutdown()
	m.Shutdown()

	select {
	case <-mgr.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown() left the manager running: children would outlive the TUI")
	}
}

// TestPickViewportFollowsCursor pins the two invariants behind the viewport
// content sync: keys patch the cached rows in place (the slice keeps its
// backing array, so the viewport sees the new strings without a rescan), and
// the frame still scrolls to keep the cursor row visible.
func TestPickViewportFollowsCursor(t *testing.T) {
	videos := make([]ytdlp.Video, 40)
	for i := range videos {
		videos[i] = ytdlp.Video{Index: i + 1, ID: fmt.Sprintf("id%02d", i), Title: fmt.Sprintf("Video %02d", i)}
	}
	m := newPickModel(&ytdlp.Playlist{Videos: videos}, 0, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	warm := m.rows
	for i := 0; i < 30; i++ {
		m, _ = m.Update(pickKeyDown())
	}
	if &m.rows[0] != &warm[0] {
		t.Error("cursor moves replaced the row slice, want in-place patches of the warmed cache")
	}
	if m.vpGen != m.rowsGen {
		t.Errorf("viewport generation = %d after patching, want the row generation %d (no rescan needed)",
			m.vpGen, m.rowsGen)
	}

	// A 14-row body leaves the viewport 12 rows, so the cursor at row 30
	// forces the window down: the marked row is visible and the first row is
	// scrolled out.
	out := plainText(m.View(100, 14))
	if !strings.Contains(out, "> [x]   31.  --:-- Video 30") {
		t.Errorf("frame = %q, want the cursor row 30 with its marker", out)
	}
	if strings.Contains(out, "Video 00") {
		t.Errorf("frame = %q, want the top row scrolled out of the viewport", out)
	}
}

// TestRuneWidthCacheMatchesLipgloss pins the rune-width cache against lipgloss
// for the script families it is meant to serve (the table must be a pure
// memo of lg.Width), and hammers it from several goroutines: the entries are
// atomics, so parallel callers must all read the same width. Run with -race to
// validate that.
func TestRuneWidthCacheMatchesLipgloss(t *testing.T) {
	runes := []rune("aZ0 ~é→πλЯ世界한글\u0301\u2764\U0001F600")

	for _, r := range runes {
		if got, want := runeWidth(r), lg.Width(string(r)); got != want {
			t.Errorf("runeWidth(%q) = %d, want %d", r, got, want)
		}
	}

	// Astral runes take the wide (sync.Map) path: the first call measures and
	// memoizes, later calls must be served from the memo with the same width
	// and no further measurement.
	astral := []rune{'\U0001F600', '\U0001F1EE', '\U0001F1E9', '\U00020000', '\U0002A6D6'}
	for _, r := range astral {
		if got, want := runeWidth(r), lg.Width(string(r)); got != want {
			t.Errorf("astral runeWidth(%q) = %d, want %d", r, got, want)
		}
		if w, ok := runeWidthWide.Load(r); !ok {
			t.Errorf("runeWidthWide has no entry for %U after runeWidth(%q), want the measured width memoized", r, r)
		} else if got := runeWidth(r); got != w.(int) {
			t.Errorf("runeWidth(%q) = %d on the memoized call, want the stored %d", r, got, w.(int))
		}
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				for _, r := range runes {
					if got, want := runeWidth(r), lg.Width(string(r)); got != want {
						t.Errorf("concurrent runeWidth(%q) = %d, want %d", r, got, want)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

// TestHelpBarMemoIsConcurrencySafe exercises the memoized legend from several
// goroutines with different widths and entry sets: HelpBar is a pure function
// of its inputs, so every caller must get the layout for the width it asked
// for, and the shared memo must stay consistent (run with -race to validate
// the mutex around it).
func TestHelpBarMemoIsConcurrencySafe(t *testing.T) {
	widths := []int{20, 40, 80}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				width := widths[(g+i)%len(widths)]
				pairs := helpPairs()
				if (g+i)%2 == 0 {
					pairs = pairs[:2]
				}
				out := HelpBar(width, pairs...)
				if out == "" {
					t.Errorf("HelpBar(%d) with %d pairs = %q, want at least the first entry", width, len(pairs), out)
					return
				}
				if got := lg.Width(out); got > width {
					t.Errorf("HelpBar(%d) width = %d, want <= %d", width, got, width)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
