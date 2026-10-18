package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"linuxhackingid.com/youtube-downloader/internal/config"
	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// formSubmitMsg is emitted when the user presses enter on the last field; the
// parent screen then persists the configuration and fetches the playlist.
type formSubmitMsg struct{}

// formCompactMinHeight is the body height below which the form switches to
// its one-line-per-field layout, so the last fields stay reachable in a short
// terminal instead of being scrolled off the bottom.
const formCompactMinHeight = 26

// formModel is the download-configuration form screen. The zero value is not
// usable; construct it with newFormModel.
type formModel struct {
	inputs    [formTextInputCount]textinput.Model
	focus     int
	quality   ytdlp.Quality
	cookies   string // "" = none
	err       string // screen-level banner (config validation or fetch errors)
	fieldErrs [formFieldCount]string
	width     int
}

// newFormModel builds the form, prefilled from cfg. The first field starts
// focused.
func newFormModel(cfg config.Config) formModel {
	def := config.Default()

	textField := func(placeholder string, limit int, value string, validate textinput.ValidateFunc) textinput.Model {
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = placeholder
		in.CharLimit = limit
		in.Validate = validate
		if value != "" {
			in.SetValue(value)
		}
		return in
	}

	m := formModel{
		quality: normalizeQuality(cfg.Quality),
		cookies: normalizeCookies(cfg.CookiesBrowser),
	}
	m.inputs[formSlotURL] = textField("https://www.youtube.com/playlist?list=...", 2048, cfg.PlaylistURL, nil)
	m.inputs[formSlotTargetDir] = textField(`C:\Users\<you>\Downloads\YouTube`, 512, cfg.TargetDir, nil)
	m.inputs[formSlotConcurrency] = textField("3", 2, positiveNumber(cfg.Concurrency), validateDigits)
	m.inputs[formSlotRangeStart] = textField("", 5, positiveNumber(cfg.RangeStart), validateDigits)
	m.inputs[formSlotRangeEnd] = textField("", 5, positiveNumber(cfg.RangeEnd), validateDigits)

	tmpl := strings.TrimSpace(cfg.FilenameTemplate)
	if tmpl == "" {
		tmpl = def.FilenameTemplate
	}
	m.inputs[formSlotTemplate] = textField("", 300, tmpl, nil)

	m.inputs[0].Focus()
	return m
}

// Init returns the focus command of the constructor-focused first field.
//
// It must not try to move focus itself: the app calls m.form.Init() for its
// command and discards the returned model, so a focus assignment here would
// silently target a copy that is thrown away. newFormModel focuses the first
// field once, at construction, and focus then persists across visits to the
// screen - which is the intended behavior: re-entering the form puts the user
// back on the field they were editing.
func (m formModel) Init() tea.Cmd {
	return m.focusCmd()
}

// Update routes keyboard input: tab/up/down move focus, left/right cycle the
// focused selector, enter advances focus or submits from the last field, and
// everything else reaches the focused textinput. Submitting runs the
// field-level checks first: on failure focus jumps to the first field that
// needs attention and no formSubmitMsg is emitted.
func (m formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.forwardToInput(msg)
	}
	switch keyMsg.String() {
	case "tab", "down":
		return m.moveFocus(1)
	case "shift+tab", "up":
		return m.moveFocus(-1)
	case "left":
		switch m.focus {
		case formFieldQuality:
			m.cycleQuality(-1)
			return m, nil
		case formFieldCookies:
			m.cycleCookies(-1)
			return m, nil
		}
	case "right":
		switch m.focus {
		case formFieldQuality:
			m.cycleQuality(1)
			return m, nil
		case formFieldCookies:
			m.cycleCookies(1)
			return m, nil
		}
	case "enter":
		if m.focus == formFieldTemplate {
			m = m.validateFields()
			if first := m.firstFieldErr(); first >= 0 {
				return m.moveFocusTo(first)
			}
			return m, func() tea.Msg { return formSubmitMsg{} }
		}
		return m.moveFocus(1)
	}
	return m.forwardToInput(msg)
}

// View renders the form for the given viewport size. A short viewport
// switches to the compact one-line-per-field layout.
func (m formModel) View(width, height int) string {
	m.width = width

	inputWidth := min(max(width-4, 30), 80)
	for slot := range m.inputs {
		m.inputs[slot].SetWidth(inputWidth)
	}

	compact := height > 0 && height < formCompactMinHeight

	var b strings.Builder
	b.WriteString(StyleTitle.Render("Download configuration"))
	b.WriteString("\n")

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(StyleError.Render("  ! " + Sanitize(m.err)))
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
	}

	for field := 0; field < formFieldCount; field++ {
		if heading := formSectionFor(field); heading != "" {
			b.WriteString(StyleHeading.Render(heading))
			b.WriteString("\n")
		}
		if compact {
			b.WriteString(m.renderFieldCompact(field))
		} else {
			b.WriteString(m.renderField(field))
		}
	}

	return b.String()
}

// Config assembles the form values into a configuration. It normalizes but
// deliberately does not validate the result.
func (m formModel) Config() config.Config {
	cfg := config.Config{
		PlaylistURL:      strings.TrimSpace(m.inputs[formSlotURL].Value()),
		TargetDir:        strings.TrimSpace(m.inputs[formSlotTargetDir].Value()),
		Quality:          m.quality,
		CookiesBrowser:   m.cookies,
		Concurrency:      parseConcurrency(m.inputs[formSlotConcurrency].Value()),
		RangeStart:       parseRange(m.inputs[formSlotRangeStart].Value()),
		RangeEnd:         parseRange(m.inputs[formSlotRangeEnd].Value()),
		FilenameTemplate: strings.TrimSpace(m.inputs[formSlotTemplate].Value()),
	}
	if cfg.FilenameTemplate == "" {
		cfg.FilenameTemplate = config.Default().FilenameTemplate
	}
	return cfg
}

// SetError stores a message to display in the error banner; an empty message
// clears it. The message is sanitized for terminal display first (newlines
// are preserved).
func (m formModel) SetError(msg string) formModel {
	msg = Sanitize(msg)
	m.err = msg
	m.fieldErrs = [formFieldCount]string{}
	return m
}

// validateFields runs the cheap, field-local checks that do not need a
// network or a filesystem, so the user gets the message next to the field
// that caused it. Screen-level problems (an invalid URL scheme, a bad
// filename template) are still reported by config.Validate through SetError.
func (m formModel) validateFields() formModel {
	m.fieldErrs = [formFieldCount]string{}

	if strings.TrimSpace(m.inputs[formSlotURL].Value()) == "" {
		m.fieldErrs[formFieldURL] = "required: paste the playlist or video URL"
	}
	if strings.TrimSpace(m.inputs[formSlotTargetDir].Value()) == "" {
		m.fieldErrs[formFieldTargetDir] = "required: downloads need a folder"
	}
	if v := strings.TrimSpace(m.inputs[formSlotConcurrency].Value()); v == "" {
		m.fieldErrs[formFieldConcurrency] = "blank means 3; type a number 1-10 to pin it"
	}
	start := parseRange(m.inputs[formSlotRangeStart].Value())
	end := parseRange(m.inputs[formSlotRangeEnd].Value())
	if start > 0 && end > 0 && start > end {
		m.fieldErrs[formFieldRangeStart] = "range start must not be after the range end"
		m.fieldErrs[formFieldRangeEnd] = "range end must not be before the range start"
	}
	return m
}

// firstFieldErr returns the focus order position of the first field with a
// validation message, or -1 when every field is clean.
func (m formModel) firstFieldErr() int {
	for field := 0; field < formFieldCount; field++ {
		if m.fieldErrs[field] != "" {
			return field
		}
	}
	return -1
}

// renderField renders one field as a label line and a value line. The focused
// field is marked with "▶" and its label carries the key style; the others
// are muted. A field-level validation message is rendered underneath in the
// error style.
func (m formModel) renderField(field int) string {
	label := formFieldLabels[field]
	value := m.renderValue(field)

	var b strings.Builder
	if m.focus == field {
		b.WriteString(StyleAccent.Render("▶ "))
		b.WriteString(StyleKey.Render(label))
		b.WriteString("\n  ")
		b.WriteString(value)
		b.WriteString("\n")
	} else {
		b.WriteString(StyleMuted.Render("  " + label))
		b.WriteString("\n  ")
		b.WriteString(value)
		b.WriteString("\n")
	}
	b.WriteString(m.renderFieldErr(field))
	return b.String()
}

// renderFieldCompact renders one field on a single line for short terminals:
// "▶ label   value". The label is truncated to a third of the width and the
// value is fitted to the columns that are left, so the row ends inside the
// terminal instead of being clipped over the input's blank padding.
func (m formModel) renderFieldCompact(field int) string {
	label := Truncate(formFieldLabels[field], max(m.width/3, 12))

	var prefix string
	if m.focus == field {
		prefix = StyleAccent.Render("▶ ") + StyleKey.Render(label) + "  "
	} else {
		prefix = StyleMuted.Render("  " + label + "  ")
	}
	m = m.fitFieldInput(field, m.width-lg.Width(prefix))

	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(m.renderValue(field))
	b.WriteString("\n")
	b.WriteString(m.renderFieldErr(field))
	return b.String()
}

// fitFieldInput narrows the textinput of field to avail columns so a compact
// row stays inside the terminal width. Selectors have no input and are left
// alone; both the layout and the value come from the returned copy, because
// the receiver is a per-frame copy of the form.
//
// bubbles renders one column more than the width it is given (the cell the
// cursor occupies), so the field is sized one short: clipping that trailing
// cell would draw a stray ellipsis after the input's blank padding.
func (m formModel) fitFieldInput(field, avail int) formModel {
	if slot := inputSlot(field); slot >= 0 {
		m.inputs[slot].SetWidth(max(avail-1, 12))
	}
	return m
}

// renderFieldErr renders the field's validation message, or nothing when the
// field is clean.
func (m formModel) renderFieldErr(field int) string {
	if m.fieldErrs[field] == "" {
		return ""
	}
	return StyleError.Render("  ! "+m.fieldErrs[field]) + "\n"
}

// renderValue renders the current value of a field; selectors gain "< >"
// arrows while focused.
func (m formModel) renderValue(field int) string {
	if slot := inputSlot(field); slot >= 0 {
		return styledInput(m.inputs[slot]).View()
	}
	var label string
	switch field {
	case formFieldQuality:
		label = m.quality.Label()
	case formFieldCookies:
		label = cookieBrowserOptions[cookiesIndex(m.cookies)]
	}
	if m.focus == field {
		return StyleValue.Render("< ") + label + StyleValue.Render(" >")
	}
	return StyleValue.Render(label)
}

// moveFocus shifts keyboard focus by delta, clamped to the field range.
func (m formModel) moveFocus(delta int) (formModel, tea.Cmd) {
	return m.moveFocusTo(min(max(m.focus+delta, 0), formFieldCount-1))
}

// moveFocusTo focuses field and returns the focus command of the newly
// focused textinput (nil for selectors).
func (m formModel) moveFocusTo(field int) (formModel, tea.Cmd) {
	if field < 0 || field >= formFieldCount || field == m.focus {
		return m, nil
	}
	m.blurFocused()
	m.focus = field
	return m, m.focusCmd()
}

// forwardToInput hands msg to the focused textinput; selectors consume their
// own keys, so messages received while they are focused are dropped. Editing
// a field clears its validation message, because the message is about the
// value the user is replacing.
func (m formModel) forwardToInput(msg tea.Msg) (formModel, tea.Cmd) {
	slot := inputSlot(m.focus)
	if slot < 0 {
		return m, nil
	}
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.fieldErrs[m.focus] = ""
	}
	var cmd tea.Cmd
	m.inputs[slot], cmd = m.inputs[slot].Update(msg)
	return m, cmd
}

// blurFocused blurs the current field when it is a textinput.
func (m *formModel) blurFocused() {
	if slot := inputSlot(m.focus); slot >= 0 {
		m.inputs[slot].Blur()
	}
}

// focusCmd focuses the current field when it is a textinput and returns its
// focus command; selectors yield nil.
func (m *formModel) focusCmd() tea.Cmd {
	if slot := inputSlot(m.focus); slot >= 0 {
		return m.inputs[slot].Focus()
	}
	return nil
}

// cycleQuality moves the quality selection by delta, wrapping around.
func (m *formModel) cycleQuality(delta int) {
	all := ytdlp.AllQualities()
	idx := 0
	for i, q := range all {
		if q == m.quality {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(all)) % len(all)
	m.quality = all[idx]
}

// cycleCookies moves the cookie browser selection by delta, wrapping around.
func (m *formModel) cycleCookies(delta int) {
	n := len(cookieBrowserOptions)
	idx := (cookiesIndex(m.cookies) + delta + n) % n
	if idx == 0 {
		m.cookies = ""
		return
	}
	m.cookies = cookieBrowserOptions[idx]
}
