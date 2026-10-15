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

// downloadModel renders live progress for a jobs.Manager run. It never
// mutates the manager; every render works from the most recent snapshot.
type downloadModel struct {
	mgr      *jobs.Manager
	cfg      config.Config
	total    int
	snapshot []jobs.JobSnapshot
	logs     []string
	finished bool
}

// newDownloadModel returns a download screen bound to mgr. total is the
// number of selected videos and feeds the header progress counter.
func newDownloadModel(mgr *jobs.Manager, cfg config.Config, total int) downloadModel {
	return downloadModel{mgr: mgr, cfg: cfg, total: total}
}

// Init starts the refresh tick loop.
func (m downloadModel) Init() tea.Cmd {
	return dlTickCmd()
}

// dlTickCmd schedules one dlTickMsg.
func dlTickCmd() tea.Cmd {
	return tea.Tick(dlTickInterval, func(time.Time) tea.Msg { return dlTickMsg{} })
}

// Update refreshes the snapshot and log tail on every tick. Once the manager
// signals completion the tick loop stops for good; all other messages are
// ignored because navigation is owned by the app.
func (m downloadModel) Update(msg tea.Msg) (downloadModel, tea.Cmd) {
	if _, ok := msg.(dlTickMsg); !ok {
		return m, nil
	}

	if m.mgr == nil {
		m.finished = true
		return m, nil
	}

	m.snapshot = m.mgr.Snapshot()
	m.logs = m.mgr.Logs()

	select {
	case <-m.mgr.Done():
		m.finished = true
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

// View renders the download screen: header counters, the overall progress
// bar, one row per job (bounded by the terminal height), the tail of the
// global log, and the key legend.
func (m downloadModel) View(width, height int) string {
	var b strings.Builder

	done, skipped, failed, running := m.counts()

	b.WriteString(StyleTitle.Render("Downloading"))
	b.WriteByte('\n')
	b.WriteString(StyleMuted.Render(fmt.Sprintf("done %d/%d • skipped %d • failed %d • running %d",
		done, m.total, skipped, failed, running)))
	b.WriteString("\n\n")

	b.WriteString(barLine("overall", m.overallPercent(), width))
	b.WriteString("\n\n")

	maxRows := max(3, height-9)
	shown := 0
	for _, s := range m.snapshot {
		if shown >= maxRows {
			break
		}
		b.WriteString(downloadRow(s, width))
		b.WriteByte('\n')
		shown++
	}
	if remaining := len(m.snapshot) - shown; remaining > 0 {
		b.WriteString(StyleMuted.Render(fmt.Sprintf("+%d more…", remaining)))
		b.WriteByte('\n')
	}

	if tail := tailNonEmpty(m.logs, 3); len(tail) > 0 {
		b.WriteByte('\n')
		for _, line := range tail {
			b.WriteString(StyleMuted.Render(Truncate(line, width-4)))
			b.WriteByte('\n')
		}
	}

	b.WriteByte('\n')
	b.WriteString(RenderHelp([2]string{"esc", "cancel all (asks y/N)"}))

	return b.String()
}

// counts tallies the visible statuses. Merging counts as running because
// both keep a worker busy.
func (m downloadModel) counts() (done, skipped, failed, running int) {
	for _, s := range m.snapshot {
		switch s.Status {
		case jobs.StatusDone:
			done++
		case jobs.StatusSkipped:
			skipped++
		case jobs.StatusFailed:
			failed++
		case jobs.StatusRunning, jobs.StatusMerging:
			running++
		}
	}
	return done, skipped, failed, running
}

// overallPercent averages the progress of every job except skipped ones,
// which are already complete and would otherwise inflate the result. Unknown
// (-1) and out-of-range percentages are clamped to 0..100.
func (m downloadModel) overallPercent() float64 {
	var sum float64
	n := 0
	for _, s := range m.snapshot {
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

// barLine renders a label, a block progress bar, and its percentage. The bar
// is clamped to 10..60 cells so it stays usable in narrow and wide terminals
// alike.
func barLine(label string, pct float64, width int) string {
	w := clampInt(width-30, 10, 60)
	pct = clampPercent(pct)

	filled := clampInt(int(pct/100*float64(w)), 0, w)
	fill := StyleAccent
	if pct >= 100 {
		fill = StyleSuccess
	}

	var bar strings.Builder
	if filled > 0 {
		bar.WriteString(fill.Render(strings.Repeat("█", filled)))
	}
	if empty := w - filled; empty > 0 {
		bar.WriteString(StyleMuted.Render(strings.Repeat("░", empty)))
	}

	return fmt.Sprintf("%-8s %s %s", label, bar.String(), FormatPercent(pct))
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

// clampInt bounds v to the inclusive range [lo, hi].
func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// downloadRow renders one job as a single line: status marker, padded index,
// truncated title, and a right-aligned detail section.
func downloadRow(s jobs.JobSnapshot, width int) string {
	left := dlStatusLabel(s.Status) + " " + fmt.Sprintf("%3d", s.Video.Index) + "  " +
		Truncate(s.Video.Title, width-40)

	right := downloadRowRight(s)
	if right == "" {
		return left
	}

	gap := width - lg.Width(left) - lg.Width(right)
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}

// downloadRowRight renders the per-status detail column: live progress for
// running jobs, a fixed 100% for finished ones, and the short error text for
// failures.
func downloadRowRight(s jobs.JobSnapshot) string {
	switch s.Status {
	case jobs.StatusRunning, jobs.StatusMerging:
		return FormatPercent(s.Percent) + " " + FormatSpeed(s.SpeedBps) + " ETA " + FormatETA(s.ETASeconds)
	case jobs.StatusDone, jobs.StatusSkipped:
		return "100%"
	case jobs.StatusFailed:
		return StyleError.Render("failed: " + Truncate(s.Err, 30))
	default:
		return ""
	}
}

// dlStatusLabel renders the compact status marker padded to four columns.
func dlStatusLabel(s jobs.Status) string {
	var label string
	switch s {
	case jobs.StatusQueued:
		label = "…"
	case jobs.StatusRunning:
		label = "RUN"
	case jobs.StatusMerging:
		label = "MRG"
	case jobs.StatusDone:
		label = "OK"
	case jobs.StatusSkipped:
		label = "SKIP"
	case jobs.StatusFailed:
		label = "ERR"
	case jobs.StatusCanceled:
		label = "CAN"
	default:
		label = "?"
	}
	if pad := 4 - lg.Width(label); pad > 0 {
		label += strings.Repeat(" ", pad)
	}
	return label
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
