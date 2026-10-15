package ytdlp

import (
	"reflect"
	"testing"
)

// Shared inputs for the BuildDownloadArgs tests. The progress template
// strings are hard-coded here on purpose: they are the wire contract with
// yt-dlp and must match what ParseProgressLine consumes.
const (
	testDownloadTemplate    = "download:DL|%(info.id)s|%(progress.status)s|%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|%(progress.speed)s|%(progress.eta)s|%(progress.percent)s"
	testPostprocessTemplate = "postprocess:PP|%(info.id)s|%(progress.status)s"
	testFilenameTemplate    = "%(playlist_index)03d - %(title)s [%(id)s].%(ext)s"
	testPlaylistURL         = "https://www.youtube.com/playlist?list=PL1"
)

// argIndex returns the index of the first exact occurrence of s in args,
// or -1 when absent.
func argIndex(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

// countArg returns how many times s appears as an element of args.
func countArg(args []string, s string) int {
	n := 0
	for _, a := range args {
		if a == s {
			n++
		}
	}
	return n
}

// hasSequence reports whether seq appears as a contiguous run in args.
func hasSequence(args []string, seq ...string) bool {
	if len(seq) == 0 {
		return true
	}
outer:
	for i := 0; i+len(seq) <= len(args); i++ {
		for j, s := range seq {
			if args[i+j] != s {
				continue outer
			}
		}
		return true
	}
	return false
}

// canonicalOpts is the full-featured option set used by the equality and
// invariant tests.
func canonicalOpts() DownloadOpts {
	return DownloadOpts{
		PlaylistURL:      testPlaylistURL,
		Video:            Video{Index: 5},
		TargetDir:        `C:\DL`,
		Quality:          Q1080,
		FilenameTemplate: testFilenameTemplate,
		CookiesBrowser:   "chrome",
		ArchivePath:      `C:\DL\.yt-dlp-archive.txt`,
	}
}

// TestBuildDownloadArgsCanonical pins the complete argv for a
// video-quality download with every optional flag set.
func TestBuildDownloadArgsCanonical(t *testing.T) {
	c := New("yt-dlp", "ffmpeg")

	want := []string{
		"--ignore-config",
		"--no-warnings",
		"--no-color",
		"--newline",
		"--progress-delta", "0.25",
		"--progress-template", testDownloadTemplate,
		"--progress-template", testPostprocessTemplate,
		"-P", `C:\DL`,
		"-o", testFilenameTemplate,
		"-f", "bv*[height<=?1080]+ba/b[height<=?1080]/b",
		"--merge-output-format", "mp4",
		"--download-archive", `C:\DL\.yt-dlp-archive.txt`,
		"--ffmpeg-location", "ffmpeg",
		"--cookies-from-browser", "chrome",
		"-I", "5",
		testPlaylistURL,
	}

	got := c.BuildDownloadArgs(canonicalOpts())
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildDownloadArgs() mismatch\n got = %q\nwant = %q", got, want)
	}
	if argIndex(got, "-x") != -1 {
		t.Errorf("BuildDownloadArgs() contains -x for a video quality: %q", got)
	}
}

// TestBuildDownloadArgsAudioOnly pins the complete argv for an audio-only
// download and asserts extraction flags replace merge flags.
func TestBuildDownloadArgsAudioOnly(t *testing.T) {
	c := New("yt-dlp", "ffmpeg")

	opts := canonicalOpts()
	opts.Quality = QAudio

	want := []string{
		"--ignore-config",
		"--no-warnings",
		"--no-color",
		"--newline",
		"--progress-delta", "0.25",
		"--progress-template", testDownloadTemplate,
		"--progress-template", testPostprocessTemplate,
		"-P", `C:\DL`,
		"-o", testFilenameTemplate,
		"-f", "ba/b",
		"-x", "--audio-format", "mp3", "--audio-quality", "0",
		"--download-archive", `C:\DL\.yt-dlp-archive.txt`,
		"--ffmpeg-location", "ffmpeg",
		"--cookies-from-browser", "chrome",
		"-I", "5",
		testPlaylistURL,
	}

	got := c.BuildDownloadArgs(opts)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildDownloadArgs() mismatch\n got = %q\nwant = %q", got, want)
	}
	if !hasSequence(got, "-x", "--audio-format", "mp3", "--audio-quality", "0") {
		t.Errorf("BuildDownloadArgs() missing -x --audio-format mp3 --audio-quality 0: %q", got)
	}
	if argIndex(got, "--merge-output-format") != -1 {
		t.Errorf("BuildDownloadArgs() contains --merge-output-format for audio-only: %q", got)
	}
}

// TestBuildDownloadArgsNoFFmpeg verifies that an empty ffmpeg path removes
// both the ffmpeg location and the merge output format.
func TestBuildDownloadArgsNoFFmpeg(t *testing.T) {
	c := New("yt-dlp", "")

	opts := DownloadOpts{
		PlaylistURL:      testPlaylistURL,
		TargetDir:        `C:\DL`,
		Quality:          QBest,
		FilenameTemplate: testFilenameTemplate,
	}

	want := []string{
		"--ignore-config",
		"--no-warnings",
		"--no-color",
		"--newline",
		"--progress-delta", "0.25",
		"--progress-template", testDownloadTemplate,
		"--progress-template", testPostprocessTemplate,
		"-P", `C:\DL`,
		"-o", testFilenameTemplate,
		"-f", "bv*+ba/b",
		testPlaylistURL,
	}

	got := c.BuildDownloadArgs(opts)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildDownloadArgs() mismatch\n got = %q\nwant = %q", got, want)
	}
	for _, flag := range []string{"--ffmpeg-location", "--merge-output-format"} {
		if argIndex(got, flag) != -1 {
			t.Errorf("BuildDownloadArgs() contains %s with empty ffmpeg path: %q", flag, got)
		}
	}
}

// TestBuildDownloadArgsOmitsEmptyOptionals verifies optional flags vanish
// when their fields are empty, even with ffmpeg configured.
func TestBuildDownloadArgsOmitsEmptyOptionals(t *testing.T) {
	c := New("yt-dlp", "ffmpeg")

	opts := DownloadOpts{
		PlaylistURL:      testPlaylistURL,
		TargetDir:        `C:\DL`,
		Quality:          QBest,
		FilenameTemplate: testFilenameTemplate,
		CookiesBrowser:   "",
		ArchivePath:      "",
		Video:            Video{Index: 0},
	}

	want := []string{
		"--ignore-config",
		"--no-warnings",
		"--no-color",
		"--newline",
		"--progress-delta", "0.25",
		"--progress-template", testDownloadTemplate,
		"--progress-template", testPostprocessTemplate,
		"-P", `C:\DL`,
		"-o", testFilenameTemplate,
		"-f", "bv*+ba/b",
		"--merge-output-format", "mp4",
		"--ffmpeg-location", "ffmpeg",
		testPlaylistURL,
	}

	got := c.BuildDownloadArgs(opts)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildDownloadArgs() mismatch\n got = %q\nwant = %q", got, want)
	}
	for _, flag := range []string{"--cookies-from-browser", "--download-archive", "-I"} {
		if argIndex(got, flag) != -1 {
			t.Errorf("BuildDownloadArgs() contains %s when its field is unset: %q", flag, got)
		}
	}
}

// TestBuildDownloadArgsInvariants verifies the positional contract across
// several configurations: the safety flags and the progress throttle lead
// the argv and the playlist URL is always the final, unsplit element.
func TestBuildDownloadArgsInvariants(t *testing.T) {
	audioOpts := canonicalOpts()
	audioOpts.Quality = QAudio

	minimalOpts := DownloadOpts{
		PlaylistURL:      testPlaylistURL,
		TargetDir:        `C:\DL`,
		Quality:          QBest,
		FilenameTemplate: testFilenameTemplate,
	}

	tests := []struct {
		name   string
		client *Client
		opts   DownloadOpts
	}{
		{"canonical video", New("yt-dlp", "ffmpeg"), canonicalOpts()},
		{"audio only without ffmpeg", New("yt-dlp", ""), audioOpts},
		{"minimal best quality", New("yt-dlp", ""), minimalOpts},
	}

	head := []string{"--ignore-config", "--no-warnings", "--no-color", "--newline", "--progress-delta", "0.25"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.client.BuildDownloadArgs(tt.opts)

			if len(got) <= len(head) {
				t.Fatalf("BuildDownloadArgs() returned %d args, want more than %d: %q", len(got), len(head), got)
			}
			for i, want := range head {
				if got[i] != want {
					t.Errorf("BuildDownloadArgs()[%d] = %q, want %q", i, got[i], want)
				}
			}
			if last := got[len(got)-1]; last != tt.opts.PlaylistURL {
				t.Errorf("BuildDownloadArgs() last element = %q, want playlist URL %q", last, tt.opts.PlaylistURL)
			}
			if n := countArg(got, tt.opts.PlaylistURL); n != 1 {
				t.Errorf("BuildDownloadArgs() contains playlist URL %d times, want exactly 1: %q", n, got)
			}
		})
	}
}

// TestBuildDownloadArgsAdversarialInputs verifies hostile strings are
// passed through as single, unsplit argv elements (no shell interpretation,
// no word splitting) and that a negative index adds no -I flag.
func TestBuildDownloadArgsAdversarialInputs(t *testing.T) {
	c := New("yt-dlp", "ffmpeg")

	const (
		evilURL     = "https://www.youtube.com/playlist?list=PL1; rm -rf / && $(id) `whoami` \"quoted\" --foo"
		evilDir     = `C:\DL; & del *.* | echo pwned`
		evilArchive = `C:\DL\a&b$(x).txt`
	)

	opts := DownloadOpts{
		PlaylistURL:      evilURL,
		Video:            Video{Index: -3},
		TargetDir:        evilDir,
		Quality:          QBest,
		FilenameTemplate: testFilenameTemplate,
		ArchivePath:      evilArchive,
	}

	got := c.BuildDownloadArgs(opts)

	if last := got[len(got)-1]; last != evilURL {
		t.Errorf("BuildDownloadArgs() last element = %q, want hostile URL passed through intact %q", last, evilURL)
	}
	if n := countArg(got, evilURL); n != 1 {
		t.Errorf("hostile URL appears %d times as an argv element, want exactly 1: %q", n, got)
	}

	if i := argIndex(got, "-P"); i == -1 || i+1 >= len(got) || got[i+1] != evilDir {
		t.Errorf("BuildDownloadArgs() did not pass hostile target dir as one element after -P: %q", got)
	}
	if i := argIndex(got, "--download-archive"); i == -1 || i+1 >= len(got) || got[i+1] != evilArchive {
		t.Errorf("BuildDownloadArgs() did not pass hostile archive path as one element: %q", got)
	}
	if argIndex(got, "-I") != -1 {
		t.Errorf("BuildDownloadArgs() contains -I for negative video index: %q", got)
	}
}

// TestBuildDownloadArgsProgressDeltaPinned locks the --progress-delta flag:
// it must appear exactly once, carry exactly "0.25" (yt-dlp parses the value
// as fractional seconds), and sit immediately after --newline so the
// throttle applies to the same output stream the parser consumes.
func TestBuildDownloadArgsProgressDeltaPinned(t *testing.T) {
	c := New("yt-dlp", "ffmpeg")

	audioOpts := canonicalOpts()
	audioOpts.Quality = QAudio

	tests := []struct {
		name string
		opts DownloadOpts
	}{
		{"canonical video", canonicalOpts()},
		{"audio only", audioOpts},
		{"minimal best", DownloadOpts{
			PlaylistURL:      testPlaylistURL,
			TargetDir:        `C:\DL`,
			Quality:          QBest,
			FilenameTemplate: testFilenameTemplate,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.BuildDownloadArgs(tt.opts)

			if n := countArg(got, "--progress-delta"); n != 1 {
				t.Fatalf("BuildDownloadArgs() contains --progress-delta %d times, want exactly 1: %q", n, got)
			}

			i := argIndex(got, "--progress-delta")
			value := ""
			if i+1 < len(got) {
				value = got[i+1]
			}
			if value != "0.25" {
				t.Fatalf("--progress-delta value = %q, want exactly %q: %q", value, "0.25", got)
			}

			newline := argIndex(got, "--newline")
			if newline == -1 || i != newline+1 {
				t.Errorf("--progress-delta at index %d, --newline at index %d: want --progress-delta immediately after --newline: %q", i, newline, got)
			}
		})
	}
}
