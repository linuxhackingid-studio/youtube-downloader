package tui

import (
	"strconv"
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

// Form field positions, in focus order.
const (
	formFieldURL = iota
	formFieldTargetDir
	formFieldQuality
	formFieldCookies
	formFieldConcurrency
	formFieldRangeStart
	formFieldRangeEnd
	formFieldTemplate
	formFieldCount
)

// Textinput slots, ordered like the fields they serve. The two selector
// fields (quality and cookies) have no textinput slot.
const (
	formSlotURL = iota
	formSlotTargetDir
	formSlotConcurrency
	formSlotRangeStart
	formSlotRangeEnd
	formSlotTemplate
	formTextInputCount
)

// formInputFields maps every textinput slot to the form field it serves.
var formInputFields = [formTextInputCount]int{
	formFieldURL,
	formFieldTargetDir,
	formFieldConcurrency,
	formFieldRangeStart,
	formFieldRangeEnd,
	formFieldTemplate,
}

// formFieldLabels holds the label rendered above each field, in focus order.
var formFieldLabels = [formFieldCount]string{
	"Playlist URL",
	"Target folder",
	"Quality",
	"Cookies from browser",
	"Concurrent downloads",
	"Range start (blank = all)",
	"Range end (blank = all)",
	"Filename template",
}

// cookieBrowserOptions lists the selectable cookie sources in cycle order.
// Index 0 ("none") is stored as an empty string.
var cookieBrowserOptions = [...]string{
	"none", "chrome", "chromium", "edge", "firefox",
	"brave", "opera", "vivaldi", "whale", "safari",
}

// Concurrency bounds applied by Config.
const (
	formDefaultConcurrency = 3
	formMinConcurrency     = 1
	formMaxConcurrency     = 10
)

// formHelpMinWidth is the smallest viewport that still leaves room for the
// help legend.
const formHelpMinWidth = 60

// formFocusStyle emphasizes the label of the field that owns keyboard focus.
var formFocusStyle = lg.NewStyle().Bold(true)

// formModel is the download-configuration form screen. The zero value is not
// usable; construct it with newFormModel.
type formModel struct {
	inputs  [formTextInputCount]textinput.Model
	focus   int
	quality ytdlp.Quality
	cookies string // "" = none
	err     string
	width   int
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

// Init focuses the first input and returns its cursor-blink command.
func (m formModel) Init() tea.Cmd {
	m.focus = formFieldURL
	return m.focusCmd()
}

// Update routes keyboard input: tab/up/down move focus, left/right cycle the
// focused selector, enter advances focus or submits from the last field, and
// everything else reaches the focused textinput.
func (m formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
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
				return m, func() tea.Msg { return formSubmitMsg{} }
			}
			return m.moveFocus(1)
		}
	}
	return m.forwardToInput(msg)
}

// View renders the form for the given viewport size.
func (m formModel) View(width, height int) string {
	m.width = width

	inputWidth := width - 4
	if inputWidth < 30 {
		inputWidth = 30
	}
	if inputWidth > 80 {
		inputWidth = 80
	}
	for slot := range m.inputs {
		m.inputs[slot].SetWidth(inputWidth)
	}

	var b strings.Builder
	b.WriteString(StyleTitle.Render("Download configuration"))
	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(StyleError.Render(m.err))
		b.WriteString("\n\n")
	}

	for field := 0; field < formFieldCount; field++ {
		b.WriteString(m.renderField(field))
	}

	if width >= formHelpMinWidth {
		b.WriteString("\n")
		b.WriteString(RenderHelp(
			[2]string{"tab/up/down", "move"},
			[2]string{"←/→", "change selector"},
			[2]string{"enter", "fetch playlist (on last field)"},
			[2]string{"esc", "back"},
		))
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
	return m
}

// moveFocus shifts keyboard focus by delta, clamped to the field range, and
// returns the focus command of the newly focused textinput (nil for
// selectors).
func (m formModel) moveFocus(delta int) (formModel, tea.Cmd) {
	next := m.focus + delta
	if next < 0 {
		next = 0
	}
	if next >= formFieldCount {
		next = formFieldCount - 1
	}
	if next == m.focus {
		return m, nil
	}
	m.blurFocused()
	m.focus = next
	return m, m.focusCmd()
}

// forwardToInput hands msg to the focused textinput; selectors consume their
// own keys, so messages received while they are focused are dropped.
func (m formModel) forwardToInput(msg tea.Msg) (formModel, tea.Cmd) {
	slot := inputSlot(m.focus)
	if slot < 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.inputs[slot], cmd = m.inputs[slot].Update(msg)
	return m, cmd
}

// renderField renders one label line and one value line. The focused field is
// prefixed with "▶" and shown plainly; the others are muted.
func (m formModel) renderField(field int) string {
	label := formFieldLabels[field]
	value := m.renderValue(field)

	if m.focus == field {
		var b strings.Builder
		b.WriteString(formFocusStyle.Render("▶ " + label))
		b.WriteString("\n  ")
		b.WriteString(value)
		b.WriteString("\n")
		return b.String()
	}
	return StyleMuted.Render("  "+label+"\n  "+value) + "\n"
}

// renderValue renders the current value of a field; selectors gain "< >"
// arrows while focused.
func (m formModel) renderValue(field int) string {
	if slot := inputSlot(field); slot >= 0 {
		return m.inputs[slot].View()
	}
	var label string
	switch field {
	case formFieldQuality:
		label = m.quality.Label()
	case formFieldCookies:
		label = cookieBrowserOptions[cookiesIndex(m.cookies)]
	}
	if m.focus == field {
		return "< " + label + " >"
	}
	return label
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

// inputSlot returns the textinput slot serving field, or -1 when the field is
// a selector.
func inputSlot(field int) int {
	for slot, f := range formInputFields {
		if f == field {
			return slot
		}
	}
	return -1
}

// validateDigits accepts empty input or ASCII digits only.
func validateDigits(value string) error {
	for _, r := range value {
		if r < '0' || r > '9' {
			// Reuse strconv's sentinel so the file needs no extra import.
			return strconv.ErrSyntax
		}
	}
	return nil
}

// normalizeQuality maps unknown values to QBest.
func normalizeQuality(q ytdlp.Quality) ytdlp.Quality {
	for _, known := range ytdlp.AllQualities() {
		if q == known {
			return q
		}
	}
	return ytdlp.QBest
}

// normalizeCookies maps unknown browser names to "" (none).
func normalizeCookies(browser string) string {
	if browser == "" {
		return ""
	}
	for _, option := range cookieBrowserOptions {
		if option == browser {
			return browser
		}
	}
	return ""
}

// cookiesIndex maps a stored cookie source to its position in
// cookieBrowserOptions; "" (none) and unknown values map to 0.
func cookiesIndex(value string) int {
	if value == "" {
		return 0
	}
	for i, option := range cookieBrowserOptions {
		if option == value {
			return i
		}
	}
	return 0
}

// parseConcurrency converts the concurrency field to 1..10, falling back to
// the default for blank or invalid input.
func parseConcurrency(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < formMinConcurrency {
		return formDefaultConcurrency
	}
	if n > formMaxConcurrency {
		return formMaxConcurrency
	}
	return n
}

// parseRange converts a range bound to a playlist index; blank or invalid
// input means "unset" (0).
func parseRange(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// positiveNumber renders n for prefill, or "" when n is unset so that the
// placeholder shows through.
func positiveNumber(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}
