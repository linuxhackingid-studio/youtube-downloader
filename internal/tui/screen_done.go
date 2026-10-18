package tui

import (
	"fmt"
	"strings"
	"time"

	lg "charm.land/lipgloss/v2"

	"linuxhackingid.com/youtube-downloader/internal/config"
	"linuxhackingid.com/youtube-downloader/internal/jobs"
	"linuxhackingid.com/youtube-downloader/internal/platform"
)

// maxFailedShown bounds how many failed entries the summary lists before the
// retry hint takes over.
const maxFailedShown = 10

// doneSummaryLabelWidth pads the summary labels into one column so the
// numbers line up.
const doneSummaryLabelWidth = 10

// doneModel is the final screen shown after every download job has reached a
// terminal state.
type doneModel struct {
	cfg     config.Config
	summary jobs.Summary
	failed  []jobs.JobSnapshot
	elapsed time.Duration
	status  string
}

// newDoneModel captures the manager's final summary, the first few failed
// jobs for display, and how long the run took.
func newDoneModel(cfg config.Config, mgr *jobs.Manager, elapsed time.Duration) doneModel {
	m := doneModel{cfg: cfg, elapsed: elapsed}
	if mgr == nil {
		return m
	}
	m.summary = mgr.Summary()
	for _, s := range mgr.Snapshot() {
		if s.Status != jobs.StatusFailed {
			continue
		}
		m.failed = append(m.failed, s)
		if len(m.failed) >= maxFailedShown {
			break
		}
	}
	return m
}

// OpenFolder opens the output folder in the native file manager and records
// the outcome on the muted status line under the summary. The launch is
// fire-and-forget, so this never blocks the event loop.
func (m doneModel) OpenFolder() doneModel {
	m.status = openFolderLine(m.cfg.TargetDir)
	return m
}

// openFolderLine turns an OpenFolder launch into the one-line status the
// summary shows.
func openFolderLine(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "no output folder configured"
	}
	if err := platform.OpenFolder(dir); err != nil {
		return "cannot open folder: " + Sanitize(err.Error())
	}
	return "opened " + dir + " in the file manager"
}

// View renders the summary panel: result counts, the output folder, the
// elapsed time, the failed list with its retry hint, and the opening status.
// The summary is short enough to fit any body height, so height is unused.
func (m doneModel) View(width, _ int) string {
	var b strings.Builder

	b.WriteString(summaryRow("completed", fmt.Sprintf("%d", m.summary.Done), &StyleSuccess))
	b.WriteByte('\n')
	b.WriteString(summaryRow("skipped", fmt.Sprintf("%d", m.summary.Skipped), &StyleMuted))
	b.WriteByte('\n')

	failed := summaryRow("failed", fmt.Sprintf("%d", m.summary.Failed), nil)
	if m.summary.Failed > 0 {
		b.WriteString(StyleError.Render(failed))
	} else {
		b.WriteString(failed)
	}
	b.WriteByte('\n')
	b.WriteString(summaryRow("canceled", fmt.Sprintf("%d", m.summary.Canceled), &StyleMuted))
	b.WriteByte('\n')
	b.WriteString(summaryRow("selected", fmt.Sprintf("%d", m.summary.Total), nil))
	b.WriteByte('\n')
	if m.elapsed > 0 {
		b.WriteString(summaryRow("elapsed", FormatETA(m.elapsed.Seconds()), &StyleMuted))
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	b.WriteString(StyleHeading.Render("Output folder"))
	b.WriteByte(' ')
	b.WriteString(StyleValue.Render(Truncate(m.cfg.TargetDir, max(width-24, 10))))
	b.WriteByte('\n')

	if len(m.failed) > 0 {
		b.WriteByte('\n')
		b.WriteString(StyleError.Render("Failed videos:"))
		b.WriteByte('\n')
		for _, s := range m.failed {
			b.WriteString(fmt.Sprintf("- %d. %s", s.Video.Index, Truncate(s.Video.Title, max(width-8, 10))))
			b.WriteByte('\n')
		}
		b.WriteString(StyleMuted.Render("press r to go back and retry; already-downloaded videos are skipped"))
		b.WriteByte('\n')
	}

	out := Panel("All downloads finished", b.String(), width, true)
	if m.status != "" {
		out += "\n" + StyleMuted.Render(Truncate(m.status, max(width-2, 10)))
	}
	return out
}

// summaryRow renders one aligned "label  value" line of the summary block.
// valueStyle colors the value; a nil style renders it plainly against the
// value color.
func summaryRow(label, value string, valueStyle *lg.Style) string {
	rendered := StyleValue.Render(value)
	if valueStyle != nil {
		rendered = valueStyle.Render(value)
	}
	return StyleHeading.Render(label) + strings.Repeat(" ", max(doneSummaryLabelWidth-lg.Width(label), 1)) + rendered
}
