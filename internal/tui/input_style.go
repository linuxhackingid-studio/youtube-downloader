// input_style.go maps the active palette onto the bubbles text inputs used by
// the form and the pick-search field. The inputs are styled at render time
// rather than at construction, for two reasons: bubbles builds its own
// defaults from a dark/light guess that cannot see this terminal, and the
// palette can flip under the app at any moment (tea.BackgroundColorMsg), which
// a style copied at construction time would silently ignore.

package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// styledInput returns in restyled for the active palette and focused state.
// It is meant to be called from a View, where the model is a copy: the
// returned value is rendered once and the caller's stored input keeps its
// previous styles for the input-handling path.
func styledInput(in textinput.Model) textinput.Model {
	in.SetStyles(inputStyles())
	return in
}

// inputStyles builds the bubbles styles record from the palette variables.
// Focused text uses the measured-value color and blurred text the dim color,
// so the field the keyboard drives stands out from the ones it does not (the
// accent is already spent on the label pointing at the focused field).
func inputStyles() textinput.Styles {
	var s textinput.Styles
	s.Focused = textinput.StyleState{
		Text:        StyleValue,
		Placeholder: StyleMuted,
		Prompt:      StyleAccent,
		Suggestion:  StyleMuted,
	}
	s.Blurred = textinput.StyleState{
		Text:        StyleDesc,
		Placeholder: StyleMuted,
		Prompt:      StyleAccent,
		Suggestion:  StyleMuted,
	}
	s.Cursor = textinput.CursorStyle{
		Color: colAccent,
		Shape: tea.CursorBlock,
		Blink: true,
	}
	return s
}
