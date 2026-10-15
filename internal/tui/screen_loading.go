package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// loadingModel is a minimal busy screen: an animated spinner followed by a
// status message.
type loadingModel struct {
	spinner spinner.Model
	message string
}

// newLoadingModel returns a loading screen showing the given message.
func newLoadingModel(message string) loadingModel {
	return loadingModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(StyleAccent)),
		message: message,
	}
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

// View renders the spinner and message, indented one level.
func (m loadingModel) View(width int) string {
	return "\n  " + m.spinner.View() + " " + m.message + "\n"
}
