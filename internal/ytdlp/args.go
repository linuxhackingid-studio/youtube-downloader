package ytdlp

import "strconv"

// DownloadOpts describes one yt-dlp download: which playlist entry to
// fetch, where to put it, and how to name and format the result.
type DownloadOpts struct {
	PlaylistURL      string  // playlist or watch URL passed to yt-dlp
	Video            Video   // entry to download; Index selects it with -I
	TargetDir        string  // -P: directory downloads are written to
	Quality          Quality // quality cap and audio/video mode
	FilenameTemplate string  // -o: yt-dlp output template
	CookiesBrowser   string  // --cookies-from-browser value; empty to omit
	ArchivePath      string  // --download-archive file; empty to omit
}

// Argument-level contracts for a download. The progress templates produce
// exactly the DL and PP lines ParseProgressLine understands; yt-dlp emits
// them without the leading "download:"/"postprocess:" selector. Template
// and parser are the wire contract with yt-dlp and must change together:
// this package is the only place that speaks it.
const (
	downloadProgressTemplate    = "download:DL|%(info.id)s|%(progress.status)s|%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|%(progress.speed)s|%(progress.eta)s|%(progress.percent)s"
	postprocessProgressTemplate = "postprocess:PP|%(info.id)s|%(progress.status)s"

	eventsCapacity = 256 // buffered progress events per download

	// progressDelta throttles yt-dlp progress emission to roughly four
	// lines per second per process, matching the TUI's 250ms tick. The
	// yt-dlp default (0) emits progress unthrottled. yt-dlp parses the
	// value as a float, so the fractional seconds are accepted.
	progressDelta = "0.25"
)

// BuildDownloadArgs returns the yt-dlp argv for o, without the executable
// path. The function is pure: it reads only o and the receiver's configured
// paths and never touches process state. The argv order is pinned by
// download_args_test.go, so new flags are appended rather than reordered.
func (c *Client) BuildDownloadArgs(o DownloadOpts) []string {
	args := []string{
		"--ignore-config",
		"--no-warnings",
		"--no-color",
		"--newline",
		"--progress-delta", progressDelta,
		"--progress-template", downloadProgressTemplate,
		"--progress-template", postprocessProgressTemplate,
		"-P", o.TargetDir,
		"-o", o.FilenameTemplate,
		"-f", o.Quality.FormatSelector(),
	}

	if !o.Quality.IsAudio() && c.FFmpegPath != "" {
		args = append(args, "--merge-output-format", "mp4")
	}
	if o.Quality.IsAudio() {
		args = append(args, "-x", "--audio-format", "mp3", "--audio-quality", "0")
	}
	if o.ArchivePath != "" {
		args = append(args, "--download-archive", o.ArchivePath)
	}
	if c.FFmpegPath != "" {
		args = append(args, "--ffmpeg-location", c.FFmpegPath)
	}
	if o.CookiesBrowser != "" {
		args = append(args, "--cookies-from-browser", o.CookiesBrowser)
	}
	if o.Video.Index > 0 {
		args = append(args, "-I", strconv.Itoa(o.Video.Index))
	}

	return append(args, o.PlaylistURL)
}
