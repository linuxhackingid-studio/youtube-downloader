package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"linuxhackingid.com/youtube-downloader/internal/config"
	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// The command constructors the root model hands to Bubble Tea. They live in
// their own file so app.go stays focused on state and routing.

// saveCmd persists the configuration off the event loop. A failed save must
// not block the workflow; the values stay in memory.
func saveCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		_ = cfg.Save()
		return nil
	}
}

// fetchCmd runs the playlist fetch off the event loop. The closure only
// captures the client, the validated configuration, the request sequence
// number and the caller-provided context; it never touches model state.
func fetchCmd(client *ytdlp.Client, cfg config.Config, seq int, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		pl, err := client.FetchPlaylist(ctx, cfg.PlaylistURL, cfg.CookiesBrowser)
		return playlistMsg{pl: pl, err: err, seq: seq}
	}
}
