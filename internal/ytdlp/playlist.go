package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
)

// Video is a single entry of a playlist, as listed by yt-dlp's flat
// playlist mode.
type Video struct {
	Index    int     // 1-based: PlaylistIndex when present && >0, else array position
	ID       string  // video id
	Title    string  // never empty; falls back to "Video <id>"
	URL      string  // JSON url; empty → "https://www.youtube.com/watch?v=" + ID
	Duration float64 // seconds; 0 = unknown
}

// Playlist is a flat listing of the videos behind a URL. A plain video
// URL yields a playlist with exactly one entry.
type Playlist struct {
	ID     string
	Title  string
	Videos []Video
}

// watchURLPrefix builds canonical watch URLs for entries whose JSON
// output carries no url field.
const watchURLPrefix = "https://www.youtube.com/watch?v="

// playlistJSON mirrors the subset of "yt-dlp -J" output that
// FetchPlaylist consumes. Pointer fields tolerate null or missing keys.
// Type and URL distinguish a single video URL (a zero-entry root of type
// "video" or with a watch URL) from a playlist whose entries were all
// unusable.
type playlistJSON struct {
	ID      *string     `json:"id"`
	Title   *string     `json:"title"`
	Type    *string     `json:"type"`
	URL     *string     `json:"url"`
	Entries []entryJSON `json:"entries"`
}

// entryJSON mirrors one playlist entry. PlaylistIndex is the authoritative
// 1-based position when yt-dlp reports it.
type entryJSON struct {
	ID            *string  `json:"id"`
	Title         *string  `json:"title"`
	URL           *string  `json:"url"`
	Duration      *float64 `json:"duration"`
	PlaylistIndex *int     `json:"playlist_index"`
}

// FetchPlaylist lists playlistURL in flat mode. cookiesBrowser, when
// non-empty, selects a browser profile for --cookies-from-browser. An
// empty playlist is not an error; a single video URL produces a
// one-entry playlist.
func (c *Client) FetchPlaylist(ctx context.Context, playlistURL, cookiesBrowser string) (*Playlist, error) {
	playlistURL = strings.TrimSpace(playlistURL)
	if err := validatePlaylistURL(playlistURL); err != nil {
		return nil, err
	}

	args := []string{"--ignore-config", "--no-warnings", "--flat-playlist", "-J", playlistURL}
	if cookiesBrowser != "" {
		args = append(args, "--cookies-from-browser", cookiesBrowser)
	}

	cmd := exec.CommandContext(ctx, c.Path, args...)
	cmd.SysProcAttr = procAttr()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open yt-dlp stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("start yt-dlp: %w", err)
	}

	var raw playlistJSON
	decodeErr := json.NewDecoder(stdout).Decode(&raw)

	// Drain the rest of stdout — a trailing newline, or everything after
	// an early decode failure — so yt-dlp cannot block on a full pipe
	// while Wait reaps it.
	_, _ = io.Copy(io.Discard, stdout)

	waitErr := cmd.Wait()

	if decodeErr != nil || waitErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
	}
	switch {
	case waitErr != nil:
		// A non-zero exit is more informative than the decode error it
		// typically causes (an empty stdout reads as io.EOF).
		return nil, exitErrorFrom(waitErr, stderr.String())
	case decodeErr != nil:
		return nil, fmt.Errorf("decode playlist JSON: %w", decodeErr)
	}

	return raw.toPlaylist(), nil
}

// validatePlaylistURL rejects URLs yt-dlp cannot fetch: empty values,
// unparsable input, and non-HTTP(S) schemes.
func validatePlaylistURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("invalid playlist URL: empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid playlist URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid playlist URL: unsupported scheme %q", u.Scheme)
	}
	return nil
}

// toPlaylist converts the tolerant JSON view into the public Playlist.
// Entries without an id are skipped. A zero-entry root is promoted to a
// one-entry playlist only when it looks like a single video URL — its type
// is "video" or its url contains "watch"; any other empty result (for
// example a playlist whose entries were all unusable) yields a Playlist
// with no videos and no error.
func (p *playlistJSON) toPlaylist() *Playlist {
	out := &Playlist{
		ID:     deref(p.ID),
		Title:  deref(p.Title),
		Videos: make([]Video, 0, len(p.Entries)),
	}

	for i, e := range p.Entries {
		id := deref(e.ID)
		if id == "" {
			continue
		}

		v := Video{
			Index: i + 1,
			ID:    id,
			Title: deref(e.Title),
			URL:   deref(e.URL),
		}
		if e.PlaylistIndex != nil && *e.PlaylistIndex > 0 {
			v.Index = *e.PlaylistIndex
		}
		if v.Title == "" {
			v.Title = "Video " + id
		}
		if v.URL == "" {
			v.URL = watchURLPrefix + id
		}
		if e.Duration != nil && *e.Duration >= 0 {
			v.Duration = *e.Duration
		}
		out.Videos = append(out.Videos, v)
	}

	if len(out.Videos) == 0 && out.ID != "" &&
		(deref(p.Type) == "video" || strings.Contains(deref(p.URL), "watch")) {
		title := out.Title
		if title == "" {
			title = "Video " + out.ID
		}
		out.Videos = []Video{{
			Index:    1,
			ID:       out.ID,
			Title:    title,
			URL:      watchURLPrefix + out.ID,
			Duration: 0,
		}}
	}

	return out
}

// deref returns the pointed-to string, or the empty string for nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
