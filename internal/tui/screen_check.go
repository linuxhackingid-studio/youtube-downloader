package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"linuxhackingid.com/youtube-downloader/internal/tools"
)

// checkModel is the setup screen: it probes for yt-dlp and ffmpeg on PATH
// and reports their versions and install locations.
type checkModel struct {
	report   tools.Report
	checking bool
	spinner  spinner.Model
	err      string
}

// checkDoneMsg carries the result of an asynchronous tool check.
type checkDoneMsg struct{ report tools.Report }

// runCheckCmd returns the command that probes yt-dlp and ffmpeg. The actual
// detection runs only when the command is executed by the Bubble Tea event
// loop, never while assembling a tea.Batch.
func runCheckCmd() tea.Cmd {
	return func() tea.Msg {
		return checkDoneMsg{report: tools.Check(context.Background())}
	}
}

// newCheckModel returns a check screen that starts probing immediately.
func newCheckModel() checkModel {
	return checkModel{
		checking: true,
		spinner:  spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(StyleAccent)),
	}
}

// Init starts the first tool check together with the spinner.
func (m checkModel) Init() tea.Cmd {
	if !m.checking {
		return nil
	}
	return tea.Batch(runCheckCmd(), m.spinner.Tick)
}

// Update handles check results, spinner ticks and the "r" re-check key.
// All other keys are deliberately ignored; navigation is owned by the app.
func (m checkModel) Update(msg tea.Msg) (checkModel, tea.Cmd) {
	switch msg := msg.(type) {
	case checkDoneMsg:
		m.report = msg.report
		m.checking = false
		return m, nil

	case spinner.TickMsg:
		// Stop the tick loop once the check has completed so late ticks
		// do not re-arm it indefinitely.
		if !m.checking {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyPressMsg:
		if msg.String() == "r" {
			m.checking = true
			return m, tea.Batch(runCheckCmd(), m.spinner.Tick)
		}
	}
	return m, nil
}

// View renders the setup screen. Width is the terminal width used to lay
// out the install-guide box.
func (m checkModel) View(width int) string {
	var b strings.Builder

	b.WriteString(StyleTitle.Render("lhi-youtube-downloader — setup check"))
	b.WriteString("\n\n")

	if m.checking {
		b.WriteString("  ")
		b.WriteString(m.spinner.View())
		b.WriteString(" Checking yt-dlp & ffmpeg...")
		b.WriteString("\n")
	} else {
		b.WriteString(toolLine(m.report.YtDlp))
		b.WriteString("\n")
		b.WriteString(toolLine(m.report.FFmpeg))
		b.WriteString("\n")
	}

	if !m.checking && !m.Ready() {
		b.WriteString("\n")
		b.WriteString(RenderBox("Install required tools", tools.InstallGuide(), width))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(RenderHelp(
		[2]string{"enter", "continue (enabled when both found)"},
		[2]string{"r", "re-check"},
		[2]string{"q", "quit"},
	))

	return b.String()
}

// Ready reports whether both required tools were found and no re-check is
// currently in flight, so callers cannot advance on a stale report.
func (m checkModel) Ready() bool {
	return !m.checking && m.report.YtDlp.Found && m.report.FFmpeg.Found
}

// ToolPaths returns the resolved executable paths for yt-dlp and ffmpeg.
func (m checkModel) ToolPaths() (ytdlpPath, ffmpegPath string) {
	return m.report.YtDlp.Path, m.report.FFmpeg.Path
}

// toolLine renders one tool status row: "[OK]  name  version  (path)" or
// "[MISSING] name — reason". When a tool was found but its version could not
// be read, the reason is rendered instead of a blank version field.
func toolLine(st tools.Status) string {
	if st.Found {
		// Version and path originate from tool output; strip terminal
		// control characters before rendering them.
		version := Sanitize(st.Version)
		if version == "" {
			if st.Err != "" {
				version = "version unknown: " + Sanitize(st.Err)
			} else {
				version = "version unknown"
			}
		}
		return StyleSuccess.Render("[OK]") + "  " + st.Name + "  " + version +
			"  (" + Sanitize(st.Path) + ")"
	}
	reason := Sanitize(st.Err)
	if reason == "" {
		reason = "not found"
	}
	return StyleError.Render("[MISSING]") + " " + st.Name + " — " + reason
}
