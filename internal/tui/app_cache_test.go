// Tests for the caches that are warmed or invalidated outside the frame that
// renders them: the pick row cache, warmed by replaying the terminal size into
// the pick model the playlist fetch creates, and the help bar and spinner
// glyph caches, reset by a theme flip. Both live at the app level, so these
// tests drive appModel (or the seams ApplyTheme calls) rather than a screen.

package tui

import (
	"strings"
	"testing"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// TestPickCacheWarmsOnPlaylistFetch pins the missing warm-up path: Bubble Tea
// delivers WindowSizeMsg once at startup and forwards it only to the active
// screen, but the pick model is created later, in the playlist-fetched
// handler, so its row cache would start cold. View runs on a receiver copy, so
// the fallback rebuild it performs on the first frame can never reach the
// stored model and every frame would pay the full O(N) rebuild plus the
// viewport's line rescan. The handler replays the last-known terminal size
// into the fresh pick model instead; removing that replay makes this test fail
// on lastRowW (0 instead of the content width) and on the viewport content.
func TestPickCacheWarmsOnPlaylistFetch(t *testing.T) {
	m := testAppModel(t, scrLoading)
	m.width, m.height = 120, 30
	m.fetchSeq = 7

	pl := &ytdlp.Playlist{Videos: []ytdlp.Video{
		{Index: 1, ID: "a", Title: "First Video"},
		{Index: 2, ID: "b", Title: "Second Video"},
	}}

	updated, _ := m.Update(playlistMsg{pl: pl, seq: 7})
	app, ok := updated.(appModel)
	if !ok {
		t.Fatalf("Update returned %T, want appModel", updated)
	}
	if app.screen != scrPick {
		t.Fatalf("screen = %v after the playlist result, want scrPick", app.screen)
	}

	// The cache must be laid out for the clamped content width - the same
	// basis View uses - and already persisted on the stored model.
	if got, want := app.pick.lastRowW, contentWidth(app.width); got != want {
		t.Errorf("pick.lastRowW = %d after the fetch, want the content width %d (replay the terminal size into the new pick model)", got, want)
	}
	if len(app.pick.rows) != len(pl.Videos) {
		t.Fatalf("len(pick.rows) = %d after the fetch, want %d", len(app.pick.rows), len(pl.Videos))
	}
	if app.pick.rowsGen == 0 {
		t.Error("pick.rowsGen = 0 after the fetch, want the warm-up rebuild to have bumped it")
	}
	if app.pick.vpGen != app.pick.rowsGen {
		t.Errorf("pick.vpGen = %d after the fetch, want the row generation %d (viewport handed the warmed rows)",
			app.pick.vpGen, app.pick.rowsGen)
	}
	if got, want := app.pick.vp.GetContent(), strings.Join(app.pick.rows, "\n"); got != want {
		t.Errorf("viewport content = %q after the fetch, want the warmed rows %q", got, want)
	}
}

// TestThemeFlipInvalidatesHelpBarAndSpinner pins the two package-level caches
// ApplyTheme owns: the memoized help bar and the pre-styled download spinner
// glyphs. Both embed the palette they were rendered with, so a flip must
// change their output (an unflipped memo would keep serving the old escape
// sequences), and a flip back must restore the original renderings.
func TestThemeFlipInvalidatesHelpBarAndSpinner(t *testing.T) {
	defer ApplyTheme(true)

	ApplyTheme(true)
	darkHelp := HelpBar(80, helpPairs()...)
	darkSpin := dlSpinStyled[0]
	if darkHelp == "" || darkSpin == "" {
		t.Fatalf("dark legend = %q and spinner glyph = %q, want both rendered", darkHelp, darkSpin)
	}
	if again := HelpBar(80, helpPairs()...); again != darkHelp {
		t.Errorf("repeated HelpBar = %q, want the memoized %q", again, darkHelp)
	}

	ApplyTheme(false)
	lightHelp := HelpBar(80, helpPairs()...)
	if lightHelp == darkHelp {
		t.Error("help bar kept the dark legend after the theme flip, want the light palette")
	}
	if dlSpinStyled[0] == darkSpin {
		t.Error("spinner glyph kept the dark accent after the theme flip, want it rebuilt")
	}

	ApplyTheme(true)
	if got := HelpBar(80, helpPairs()...); got != darkHelp {
		t.Errorf("help bar = %q after flipping back to dark, want the original %q", got, darkHelp)
	}
	if dlSpinStyled[0] != darkSpin {
		t.Errorf("spinner glyph = %q after flipping back to dark, want the original %q", dlSpinStyled[0], darkSpin)
	}
}
