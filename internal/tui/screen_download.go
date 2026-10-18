package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"linuxhackingid.com/youtube-downloader/internal/config"
	"linuxhackingid.com/youtube-downloader/internal/jobs"
)

// dlTickMsg is the periodic refresh trigger for the download screen. The
// model samples the manager on every tick and re-arms itself until the run
// finishes.
type dlTickMsg struct{}

// dlTickInterval is how often the download screen samples the manager. It is
// short enough for smooth progress and long enough to avoid flooding the
// event loop with redundant work.
const dlTickInterval = 250 * time.Millisecond

// dlSpinFrames is the per-job spinner cycle, driven by the tick counter so
// running rows animate without a second tick loop.
var dlSpinFrames = [...]string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// dlSpinStyled holds dlSpinFrames pre-rendered with the active accent color, so
// a running row copies a finished string instead of styling a glyph per row per
// frame. ApplyTheme rebuilds it, because the rendered strings embed the colors
// of the palette they were built with.
var dlSpinStyled [len(dlSpinFrames)]string

// rebuildSpinGlyphs re-renders the spinner cycle for the active palette. It is
// called by ApplyTheme, which every screen already calls on a theme flip.
func rebuildSpinGlyphs() {
	for i, g := range dlSpinFrames {
		dlSpinStyled[i] = StyleAccent.Render(g)
	}
}

// Download screen layout constants.
const (
	// dlMiniBarCells is the width of the compact per-job bar.
	dlMiniBarCells = 8
	// dlLogTailLines is how many non-empty log lines the tail panel shows.
	dlLogTailLines = 4
	// dlLogPanelMinHeight is the shortest body that still fits a job list
	// and the log tail; below it the log panel is dropped so the jobs stay
	// visible.
	dlLogPanelMinHeight = 14

	// dlRowMinTitle is the shortest title slice still worth showing beside a
	// detail column. When the full detail column would squeeze the title
	// below it, the column degrades to its compact form so the row stays
	// legible instead of being clipped mid-detail.
	dlRowMinTitle = 12
)

// dlViewCache memoizes one rendered frame. downloadModel is copied on every
// Update and View call, so the cache lives behind a pointer the copies share;
// the TUI renders on the single Bubble Tea event goroutine, so no lock is
// needed. The key is (revision, content width, content height, elapsed
// second, spinner frame): a frame that differs in none of them cannot render
// differently, and rebuilding its strings is pure waste.
type dlViewCache struct {
	valid  bool
	rev    uint64
	width  int
	height int
	sec    int
	spin   int
	view   string

	// Metrics captured alongside the view, so the window title costs
	// nothing: pct is the overall percentage, done the completed job count
	// and total the number of selected videos.
	pct   float64
	done  int
	total int
}

// downloadModel renders live progress for a jobs.Manager run. It never
// mutates the manager; every render works from the most recent snapshot.
type downloadModel struct {
	mgr      *jobs.Manager
	cfg      config.Config
	total    int
	snapshot []jobs.JobSnapshot
	logs     []string
	finished bool
	started  time.Time
	revision uint64
	spin     int
	cache    *dlViewCache
}

// newDownloadModel returns a download screen bound to mgr. total is the
// number of selected videos and feeds the header progress counter. The first
// snapshot is taken here so the very first frame already lists every job.
func newDownloadModel(mgr *jobs.Manager, cfg config.Config, total int) downloadModel {
	m := downloadModel{
		mgr:     mgr,
		cfg:     cfg,
		total:   total,
		started: time.Now(),
		cache:   &dlViewCache{},
	}
	if mgr != nil {
		m.revision = mgr.Revision()
		m.snapshot = mgr.Snapshot()
		m.logs = mgr.Logs()
	}
	return m
}

// Init starts the refresh tick loop.
func (m downloadModel) Init() tea.Cmd {
	return dlTickCmd()
}

// dlTickCmd schedules one dlTickMsg.
func dlTickCmd() tea.Cmd {
	return tea.Tick(dlTickInterval, func(time.Time) tea.Msg { return dlTickMsg{} })
}

// Update refreshes the snapshot and log tail, but only when the manager's
// revision moved: copying thousands of job records and log lines on every
// 250ms tick for a run whose state did not change is exactly the waste the
// revision counter exists to avoid. Once the manager signals completion the
// tick loop stops for good; all other messages are ignored because navigation
// is owned by the app.
func (m downloadModel) Update(msg tea.Msg) (downloadModel, tea.Cmd) {
	if _, ok := msg.(dlTickMsg); !ok {
		return m, nil
	}

	if m.mgr == nil {
		m.finished = true
		return m, nil
	}

	m.spin++
	if rev := m.mgr.Revision(); rev != m.revision {
		m.revision = rev
		m.snapshot = m.mgr.Snapshot()
		m.logs = m.mgr.Logs()
		m.invalidate()
	}

	select {
	case <-m.mgr.Done():
		// Take one final, complete sample: the last revision bump and the
		// closed done channel can arrive in the same tick.
		m.revision = m.mgr.Revision()
		m.snapshot = m.mgr.Snapshot()
		m.logs = m.mgr.Logs()
		m.finished = true
		m.invalidate()
		return m, nil
	default:
		return m, dlTickCmd()
	}
}

// Finished reports whether the manager has completed every download. The app
// uses it to move on to the summary screen.
func (m downloadModel) Finished() bool {
	return m.finished
}

// Elapsed reports how long the run has been going, for the summary screen.
func (m downloadModel) Elapsed() time.Duration {
	if m.started.IsZero() {
		return 0
	}
	return time.Since(m.started)
}

// Invalidate drops the memoized frame. The app calls it when the palette
// changes, because the cached string embeds the colors it was built with.
func (m downloadModel) Invalidate() {
	m.invalidate()
}

// invalidate drops the memoized frame on the shared cache.
func (m downloadModel) invalidate() {
	if m.cache != nil {
		m.cache.valid = false
	}
}

// Title renders the terminal window title, for example
// "lhi-youtube-downloader - 42% (3/10)". It reports the metrics captured with
// the last rendered frame, so the title bar doubles as a progress indicator
// while the window is in the background.
func (m downloadModel) Title() string {
	pct, done, total := 0.0, 0, m.total
	if c := m.cache; c != nil && c.valid {
		pct, done = c.pct, c.done
		if c.total > 0 {
			total = c.total
		}
	}
	return fmt.Sprintf("%s - %s (%d/%d)", appName, strings.TrimSpace(FormatPercent(pct)), done, total)
}

// View renders the download screen: header counters, the overall progress
// bar, one row per job (bounded by the terminal height), the tail of the
// global log, and - while a job is running - a spinner glyph per running row.
//
// The frame is memoized by (revision, width, height, elapsed second, spinner
// frame), so repeated renders between ticks cost one map-free comparison.
func (m downloadModel) View(width, height int) string {
	if m.mgr == nil {
		return StyleMuted.Render("no download is running")
	}

	rev := m.mgr.Revision()
	sec := int(m.Elapsed().Seconds())
	spin := 0
	if hasRunning(m.snapshot) {
		spin = m.spin
	}

	c := m.cache
	if c != nil && c.valid && c.rev == rev && c.sec == sec && c.spin == spin &&
		c.width == width && c.height == height {
		return c.view
	}

	snap, logs := m.snapshot, m.logs
	if rev != m.revision {
		// The manager changed since the last tick sampled it; the next
		// tick would fetch this anyway, so take the fresh copy now and
		// render the current state instead of the stale one.
		snap, logs = m.mgr.Snapshot(), m.mgr.Logs()
	}

	view := m.render(width, height, snap, logs)
	if c != nil {
		c.valid = true
		c.rev, c.sec, c.spin, c.width, c.height = rev, sec, spin, width, height
		c.view = view
		c.pct, c.done, c.total = overallPercent(snap), tally(snap).done, m.total
	}
	return view
}

// render builds the frame from an explicit snapshot and log tail.
func (m downloadModel) render(width, height int, snap []jobs.JobSnapshot, logs []string) string {
	c := tally(snap)

	var b strings.Builder
	b.WriteString(StyleTitle.Render("Downloading"))
	b.WriteString(StyleMuted.Render(fmt.Sprintf("  %d of %d complete", c.done+c.skipped, m.total)))
	b.WriteString("\n")
	b.WriteString(StyleMuted.Render(fmt.Sprintf(
		"done %d • running %d • queued %d • failed %d • skipped %d • elapsed %s",
		c.done, c.running, c.queued, c.failed, c.skipped, FormatETA(m.Elapsed().Seconds()))))
	b.WriteString("\n\n")

	b.WriteString(Bar("overall", overallPercent(snap), width))
	b.WriteString("\n\n")

	// Leave room for the log panel while there is height to spare; on a
	// short body the job rows win.
	rowBudget := max(3, height-9)
	withLog := height >= dlLogPanelMinHeight
	if !withLog {
		rowBudget = max(3, height-7)
	}

	shown := 0
	for _, s := range snap {
		if shown >= rowBudget {
			break
		}
		b.WriteString(m.jobRow(s, width))
		b.WriteByte('\n')
		shown++
	}
	if remaining := len(snap) - shown; remaining > 0 {
		b.WriteString(StyleMuted.Render(fmt.Sprintf("+%d more…", remaining)))
		b.WriteByte('\n')
	}

	if withLog {
		if tail := tailNonEmpty(logs, dlLogTailLines); len(tail) > 0 {
			// Log lines are yt-dlp output: untrusted. frame() only
			// truncates a body line whose measured width exceeds the
			// panel, and ANSI escapes measure as zero columns, so a short
			// line carrying an OSC/CSI sequence would slip through
			// verbatim. Sanitize every line first, then join: Sanitize
			// strips escapes but must not be trusted to preserve the
			// newlines that separate the lines of the panel body.
			b.WriteByte('\n')
			b.WriteString(Panel("log", strings.Join(SanitizeLines(tail), "\n"), width, false))
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// dlCounts tallies the visible statuses.
type dlCounts struct {
	done, skipped, failed, running, queued int
}

// tally counts the job statuses. Merging counts as running because both keep
// a worker busy.
func tally(snap []jobs.JobSnapshot) dlCounts {
	var c dlCounts
	for _, s := range snap {
		switch s.Status {
		case jobs.StatusDone:
			c.done++
		case jobs.StatusSkipped:
			c.skipped++
		case jobs.StatusFailed:
			c.failed++
		case jobs.StatusRunning, jobs.StatusMerging:
			c.running++
		default:
			c.queued++
		}
	}
	return c
}

// hasRunning reports whether any job still keeps a worker busy, which is what
// makes the per-job spinner worth animating.
func hasRunning(snap []jobs.JobSnapshot) bool {
	for _, s := range snap {
		if s.Status == jobs.StatusRunning || s.Status == jobs.StatusMerging {
			return true
		}
	}
	return false
}

// overallPercent averages the progress of every job except skipped ones,
// which are already complete and would otherwise inflate the result. Unknown
// (-1) and out-of-range percentages are clamped to 0..100.
func overallPercent(snap []jobs.JobSnapshot) float64 {
	var sum float64
	n := 0
	for _, s := range snap {
		if s.Status == jobs.StatusSkipped {
			continue
		}
		sum += clampPercent(s.Percent)
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// clampPercent normalizes a job percentage to 0..100, mapping the unknown
// (-1) sentinel to 0.
func clampPercent(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// jobRow renders one job as a single line: status marker, spinner glyph,
// padded index, the title, and a right-aligned detail section. The title is
// truncated to exactly the space the detail column leaves over, so wide
// terminals show the full title instead of a fixed-width cut, and the detail
// column degrades to its compact form before the title becomes unreadable.
func (m downloadModel) jobRow(s jobs.JobSnapshot, width int) string {
	glyph := "  "
	if s.Status == jobs.StatusRunning || s.Status == jobs.StatusMerging {
		glyph = dlSpinStyled[m.spin%len(dlSpinStyled)]
	}
	prefix := dlStatusLabel(s.Status) + glyph + " " + fmt.Sprintf("%3d. ", s.Video.Index)

	right := dlRowRight(s)
	if right != "" && width-lg.Width(prefix)-lg.Width(right) < dlRowMinTitle {
		if short := dlRowRightShort(s); short != "" && lg.Width(short) < lg.Width(right) {
			right = short
		}
	}

	budget := width - lg.Width(prefix)
	if right != "" {
		budget -= lg.Width(right) + 2
	}
	title := Truncate(s.Video.Title, max(10, budget))

	if right == "" {
		return clipStyled(prefix+title, width)
	}
	gap := width - lg.Width(prefix) - lg.Width(title) - lg.Width(right)
	if gap < 2 {
		gap = 2
	}
	return clipStyled(prefix+title+strings.Repeat(" ", gap)+right, width)
}

// dlRowRight renders the per-status detail column: live progress for running
// jobs, a fixed 100% for finished ones, and the short error text for
// failures.
func dlRowRight(s jobs.JobSnapshot) string {
	switch s.Status {
	case jobs.StatusRunning, jobs.StatusMerging:
		pct := clampPercent(s.Percent)
		return FormatPercent(pct) + " " + MiniBar(pct, dlMiniBarCells) + " " +
			FormatSpeed(s.SpeedBps) + " ETA " + FormatETA(s.ETASeconds)
	case jobs.StatusDone:
		return StyleSuccess.Render("100%")
	case jobs.StatusSkipped:
		// Skipped jobs were already on disk: nothing was transferred, so
		// claiming "100%" here would misread as a fresh download.
		return StyleMuted.Render("already on disk")
	case jobs.StatusFailed:
		return StyleError.Render("failed: " + Truncate(s.Err, 30))
	case jobs.StatusQueued:
		return StyleMuted.Render("queued")
	default:
		return ""
	}
}

// dlRowRightShort renders the compact detail column, shown when the full one
// would leave no useful room for the title: it keeps only the single number
// that matters for the status and drops the derivative text.
func dlRowRightShort(s jobs.JobSnapshot) string {
	switch s.Status {
	case jobs.StatusRunning, jobs.StatusMerging:
		return FormatPercent(clampPercent(s.Percent))
	case jobs.StatusDone:
		return StyleSuccess.Render("100%")
	case jobs.StatusSkipped:
		return StyleMuted.Render("already on disk")
	case jobs.StatusFailed:
		return StyleError.Render("failed")
	case jobs.StatusQueued:
		return StyleMuted.Render("queued")
	default:
		return ""
	}
}

// dlStatusLabel renders the compact status marker padded to four columns and
// colored by outcome.
func dlStatusLabel(s jobs.Status) string {
	label, style := "?", StyleMuted
	switch s {
	case jobs.StatusQueued:
		label, style = "…", StyleMuted
	case jobs.StatusRunning:
		label, style = "RUN", StyleAccent
	case jobs.StatusMerging:
		label, style = "MRG", StyleAccent
	case jobs.StatusDone:
		label, style = "OK", StyleSuccess
	case jobs.StatusSkipped:
		label, style = "SKIP", StyleMuted
	case jobs.StatusFailed:
		label, style = "ERR", StyleError
	case jobs.StatusCanceled:
		label, style = "CAN", StyleMuted
	}
	return style.Render(label) + strings.Repeat(" ", max(4-lg.Width(label), 0))
}

// tailNonEmpty returns at most the last n non-empty lines of lines in
// chronological order, so the freshest output appears at the bottom.
func tailNonEmpty(lines []string, n int) []string {
	if n <= 0 {
		return nil
	}
	out := make([]string, 0, n)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			out = append(out, line)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
