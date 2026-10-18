package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
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

// maxPlaylistEntries caps how many entries one flat listing may carry. Decode
// memory is bounded separately by maxPlaylistJSONBytes; this cap bounds the
// per-entry work that follows decoding (conversion, queueing, TUI rows) and
// turns an absurd listing into an explicit error instead. A hostile or
// degenerate playlist this large is paged by the caller.
const maxPlaylistEntries = 10000

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

// maxPlaylistJSONBytes caps the listing FetchPlaylist decodes. The entry cap
// alone does not bound decode memory: a hostile http(s) host can stream a body
// with few entries but enormous strings, and the entry count is only known
// once decoding has finished. Reads made through newPlaylistReader stop at
// this many bytes and report errPlaylistTooLarge, so decoding can hold at most
// this much data.
const maxPlaylistJSONBytes = 256 << 20 // 256 MiB

// errPlaylistTooLarge reports a playlist listing that grew past the byte cap
// (maxPlaylistJSONBytes) while it was being read.
var errPlaylistTooLarge = errors.New("playlist response exceeds size limit")

// cappedReader hands out at most limit bytes of r and then fails with
// errPlaylistTooLarge if r has anything more to give. It deliberately differs
// from io.LimitReader: LimitReader truncates and reports io.EOF, which the
// JSON decoder would surface as a confusing "unexpected end of JSON input",
// while cappedReader names the real cause. Reads that stay within the limit
// are byte-for-byte identical to r's.
type cappedReader struct {
	r     io.Reader
	limit int64 // maximum number of bytes handed out
	read  int64 // bytes handed out so far
}

// newPlaylistReader wraps r so that decoding cannot consume more than limit
// bytes of it. FetchPlaylist passes maxPlaylistJSONBytes; tests pass a tiny
// limit to exercise the cap without a 256 MiB fixture.
func newPlaylistReader(r io.Reader, limit int64) io.Reader {
	return &cappedReader{r: r, limit: limit}
}

// Read passes data through until the limit is reached. Once the budget is
// spent it probes one further byte to tell a stream that ended exactly at the
// limit from one that continues past it; only the latter is an error, and the
// probe byte is dropped along with the rest of the oversized stream.
func (c *cappedReader) Read(p []byte) (int, error) {
	if c.read >= c.limit {
		var probe [1]byte
		n, err := c.r.Read(probe[:])
		if n > 0 {
			return 0, errPlaylistTooLarge
		}
		return 0, err
	}

	if remaining := c.limit - c.read; int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
}

// maxPlaylistStderrBytes caps how much of yt-dlp's stderr FetchPlaylist
// retains. Only the last lines are ever reported (exitErrorFrom keeps ten), so
// keeping the tail is enough, and the cap stops a hostile child from growing
// the buffer without bound.
const maxPlaylistStderrBytes = 64 << 10 // 64 KiB

// tailBuffer is an io.Writer that keeps only the last max bytes written to
// it, dropping the oldest retained bytes as new ones arrive. FetchPlaylist
// uses it as cmd.Stderr: writes are always accepted in full, so the exec
// stderr copier never blocks, while memory stays bounded no matter how much
// the child streams. The copier goroutine is the only writer and the buffer is
// read only after cmd.Wait has reaped it, so no lock is needed.
type tailBuffer struct {
	buf []byte
	max int
}

// newTailBuffer returns a tailBuffer that retains at most max bytes.
func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

// Write reports len(p) even when bytes are dropped, so the writer is never
// asked to retry.
func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.max <= 0 || n == 0 {
		return n, nil
	}

	if n >= b.max {
		// This write alone fills the buffer; keep only its tail.
		b.buf = append(b.buf[:0], p[n-b.max:]...)
		return n, nil
	}
	if excess := len(b.buf) + n - b.max; excess > 0 {
		// Shift the retained bytes left to make room for all of p.
		b.buf = append(b.buf[:0], b.buf[excess:]...)
	}
	b.buf = append(b.buf, p...)
	return n, nil
}

// String returns the retained tail.
func (b *tailBuffer) String() string { return string(b.buf) }

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
	stderr := newTailBuffer(maxPlaylistStderrBytes)
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("start yt-dlp: %w", err)
	}

	var raw playlistJSON
	decodeErr := json.NewDecoder(newPlaylistReader(stdout, maxPlaylistJSONBytes)).Decode(&raw)

	// Drain the rest of stdout - a trailing newline, or everything after
	// an early decode failure - so yt-dlp cannot block on a full pipe
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

	if err := raw.checkEntryLimit(); err != nil {
		return nil, err
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

// checkEntryLimit rejects a listing that carries more entries than
// maxPlaylistEntries, before toPlaylist converts any of them. The error is
// deterministic and needs no wrapping: there is no underlying failure to
// preserve.
func (p *playlistJSON) checkEntryLimit() error {
	if len(p.Entries) > maxPlaylistEntries {
		return fmt.Errorf("playlist too large (>%d entries)", maxPlaylistEntries)
	}
	return nil
}

// toPlaylist converts the tolerant JSON view into the public Playlist.
// Entries without an id are skipped. A zero-entry root is promoted to a
// one-entry playlist only when it looks like a single video URL - its type
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
