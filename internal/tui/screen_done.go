package tui

import (
	"fmt"
	"strings"

	"linuxhackingid.com/youtube-downloader/internal/config"
	"linuxhackingid.com/youtube-downloader/internal/jobs"
)

// maxFailedShown bounds how many failed entries the summary lists before the
// retry hint takes over.
const maxFailedShown = 10

// doneModel is the final screen shown after every download job has reached a
// terminal state.
type doneModel struct {
	cfg     config.Config
	summary jobs.Summary
	failed  []jobs.JobSnapshot
}

// newDoneModel captures the manager's final summary and the first few failed
// jobs for display.
func newDoneModel(cfg config.Config, mgr *jobs.Manager) doneModel {
	m := doneModel{cfg: cfg}
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

// View renders the summary: result counts, the output folder, the failed
// list with a retry hint, and the key legend.
func (m doneModel) View(width, height int) string {
	var b strings.Builder

	b.WriteString(StyleTitle.Render("All downloads finished"))
	b.WriteString("\n\n")

	b.WriteString(StyleSuccess.Render(fmt.Sprintf("completed: %d", m.summary.Done)))
	b.WriteByte('\n')
	b.WriteString(StyleMuted.Render(fmt.Sprintf("skipped: %d", m.summary.Skipped)))
	b.WriteByte('\n')

	failedLine := fmt.Sprintf("failed: %d", m.summary.Failed)
	if m.summary.Failed > 0 {
		b.WriteString(StyleError.Render(failedLine))
	} else {
		b.WriteString(failedLine)
	}
	b.WriteByte('\n')
	b.WriteString(StyleMuted.Render(fmt.Sprintf("canceled: %d", m.summary.Canceled)))
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("total selected: %d", m.summary.Total))
	b.WriteString("\n\n")

	b.WriteString("Output folder: ")
	b.WriteString(Truncate(m.cfg.TargetDir, max(width-15, 10)))
	b.WriteByte('\n')

	if len(m.failed) > 0 {
		b.WriteByte('\n')
		b.WriteString(StyleError.Render("Failed videos:"))
		b.WriteByte('\n')
		for _, s := range m.failed {
			b.WriteString(fmt.Sprintf("- %d. %s", s.Video.Index, Truncate(s.Video.Title, max(width-8, 10))))
			b.WriteByte('\n')
		}
		b.WriteString("Press r to go back and retry (already-downloaded videos are skipped automatically).")
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	b.WriteString(RenderHelp(
		[2]string{"enter", "quit"},
		[2]string{"r", "download more (back to config)"},
	))

	return b.String()
}
