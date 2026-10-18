// screen_form_fields.go holds the form's field metadata: the focus order,
// the labels and section headings, the selector options, and the parse and
// normalize helpers the form model uses. Keeping it separate from the
// rendering code in screen_form.go makes both files small enough to read in
// one go.
package tui

import (
	"strconv"
	"strings"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// Form field positions, in focus order. The order is a contract: the
// end-to-end journeys advance focus with tab and submit with enter on the
// last field.
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

// formFieldSections names the section each field belongs to, in focus order.
// Fields of the same section share one heading, so the grouping never
// disturbs the tab order.
var formFieldSections = [formFieldCount]string{
	"Source",
	"Output",
	"Download",
	"Download",
	"Download",
	"Selection",
	"Selection",
	"File name",
}

// formSectionFor returns the heading that must be rendered above field, or
// the empty string when the previous field already opened that section.
func formSectionFor(field int) string {
	if field < 0 || field >= formFieldCount {
		return ""
	}
	if field == 0 || formFieldSections[field] != formFieldSections[field-1] {
		return formFieldSections[field]
	}
	return ""
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
