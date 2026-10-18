package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// loadingModel is the busy screen shown while the playlist fetch runs: an
// animated spinner, the status message, the elapsed time and the cancel hint.
type loadingModel struct {
	spinner spinner.Model
	message string
	started time.Time
}

// newLoadingModel returns a loading screen showing the given message. The
// elapsed counter starts now, so the spinner ticks double as its clock.
func newLoadingModel(message string) loadingModel {
	return loadingModel{
		spinner: newLoadingSpinner(),
		message: message,
		started: time.Now(),
	}
}

// newLoadingSpinner builds the fetch spinner with the active palette.
// Spinners copy the style they are built with, so a theme flip rebuilds them
// through Retheme.
func newLoadingSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(StyleAccent))
}

// Retheme rebuilds the spinner so it carries the active palette. Spinners
// copy the style they are built with, so a theme flip has to rebuild them.
func (m loadingModel) Retheme() loadingModel {
	m.spinner = newLoadingSpinner()
	return m
}

// Tick restarts the spinner animation after a Retheme, for the same reason
// the check screen restarts its probe spinner.
func (m loadingModel) Tick() tea.Cmd {
	return m.spinner.Tick
}

// Init starts the spinner tick loop.
func (m loadingModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update advances the spinner on tick messages; everything else is ignored.
func (m loadingModel) Update(msg tea.Msg) (loadingModel, tea.Cmd) {
	if _, ok := msg.(spinner.TickMsg); !ok {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// View renders the fetch progress inside a titled box: spinner, message,
// elapsed seconds and the cancel hint.
func (m loadingModel) View(width int) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(m.spinner.View())
	b.WriteString(" ")
	b.WriteString(m.message)
	b.WriteString("\n\n")
	b.WriteString(StyleMuted.Render("  elapsed " + FormatETA(m.elapsedSeconds())))
	b.WriteString("\n")
	b.WriteString(StyleMuted.Render("  esc cancels the fetch and returns to the form"))

	return RenderBox("Loading playlist", b.String(), width)
}

// elapsedSeconds reports how long the fetch has been running, or the unknown
// sentinel (-1) for a zero-value model so the caller never divides by a
// zero timestamp.
func (m loadingModel) elapsedSeconds() float64 {
	if m.started.IsZero() {
		return -1
	}
	return time.Since(m.started).Seconds()
}
