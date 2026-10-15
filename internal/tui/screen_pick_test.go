// Tests for the pick screen's incremental row cache: WindowSizeMsg warms it,
// cursor and selection keys patch only the rows they affect, and a/n rebuild
// it wholesale. Plain substring assertions only; no golden files.

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// pickKeyDown and pickKeySpace build the special keys the pick screen matches
// on: both are C0 keys, so only Code is set and Text stays empty. String()
// then yields "down" and "space" respectively (see bubbletea v2 key.go).
func pickKeyDown() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyDown} }
func pickKeySpace() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace} }

// pickKeyRune builds the key press a terminal delivers for a plain letter:
// the rune in both Code and Text, so String() returns the letter itself.
func pickKeyRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// newPickTestModel builds the three-entry pick screen used by the cache
// tests, with every video selected and the cursor on row 0.
func newPickTestModel() pickModel {
	pl := &ytdlp.Playlist{
		Title: "T",
		Videos: []ytdlp.Video{
			{Index: 1, ID: "a", Title: "One"},
			{Index: 2, ID: "b", Title: "Two"},
			{Index: 3, ID: "c", Title: "Three"},
		},
	}
	return newPickModel(pl, 0, 0)
}

// TestPickModelRowCache pins the revived row cache contract: the window size
// message warms it, and keys patch the affected rows in place instead of
// rebuilding the full list.
func TestPickModelRowCache(t *testing.T) {
	m := newPickTestModel()

	// WindowSizeMsg warms the cache for the layout width, and it persists
	// on the returned model so later keys can patch single rows.
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if m.lastRowW != 100 {
		t.Fatalf("lastRowW = %d after WindowSizeMsg, want 100", m.lastRowW)
	}
	if len(m.rows) != 3 {
		t.Fatalf("len(rows) = %d after WindowSizeMsg, want 3", len(m.rows))
	}

	// The cursor starts on row 0: "> " marks it, and no other row carries
	// the marker.
	if !strings.Contains(m.rows[0], ">") {
		t.Errorf("rows[0] = %q, want cursor marker %q", m.rows[0], ">")
	}
	if strings.Contains(m.rows[1], ">") {
		t.Errorf("rows[1] = %q, must not carry the cursor marker", m.rows[1])
	}

	// down moves the cursor to row 1 and patches the two affected rows.
	m, _ = m.Update(pickKeyDown())
	if m.cursor != 1 {
		t.Fatalf("cursor = %d after down, want 1", m.cursor)
	}
	if !strings.Contains(m.rows[1], ">") {
		t.Errorf("rows[1] = %q after down, want cursor marker %q", m.rows[1], ">")
	}
	if strings.Contains(m.rows[0], ">") {
		t.Errorf("rows[0] = %q after down, must not carry the cursor marker", m.rows[0])
	}
	if len(m.rows) != 3 {
		t.Fatalf("len(rows) = %d after down, want 3", len(m.rows))
	}

	// space toggles row 1 off and patches its checkbox.
	m, _ = m.Update(pickKeySpace())
	if m.selected[1] {
		t.Error("selected[1] = true after space, want false")
	}
	if !strings.Contains(m.rows[1], "[ ]") {
		t.Errorf("rows[1] = %q after space, want unchecked checkbox %q", m.rows[1], "[ ]")
	}
	if strings.Contains(m.rows[1], "[x]") {
		t.Errorf("rows[1] = %q after space, must not keep checked checkbox %q", m.rows[1], "[x]")
	}
	if !m.selected[0] || !m.selected[2] {
		t.Errorf("selected = %v after space, want rows 0 and 2 still selected", m.selected)
	}

	// a selects everything again and rebuilds the cache.
	m, _ = m.Update(pickKeyRune('a'))
	if !m.selected[0] || !m.selected[1] || !m.selected[2] {
		t.Errorf("selected = %v after a, want all true", m.selected)
	}
	if !strings.Contains(m.rows[1], "[x]") {
		t.Errorf("rows[1] = %q after a, want checked checkbox %q", m.rows[1], "[x]")
	}
	if len(m.rows) != 3 {
		t.Fatalf("len(rows) = %d after a, want 3", len(m.rows))
	}

	// n clears everything and rebuilds the cache as well.
	m, _ = m.Update(pickKeyRune('n'))
	if m.selected[0] || m.selected[1] || m.selected[2] {
		t.Errorf("selected = %v after n, want all false", m.selected)
	}
	if !strings.Contains(m.rows[0], "[ ]") {
		t.Errorf("rows[0] = %q after n, want unchecked checkbox %q", m.rows[0], "[ ]")
	}
	if strings.Contains(m.rows[0], "[x]") {
		t.Errorf("rows[0] = %q after n, must not keep checked checkbox %q", m.rows[0], "[x]")
	}
	if len(m.rows) != 3 {
		t.Fatalf("len(rows) = %d after n, want 3", len(m.rows))
	}
}
