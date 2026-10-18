package ytdlp

import "testing"

// Sinks keep the benchmarked results live so the compiler cannot eliminate
// the calls under test. Names are unique to this file so concurrent test
// files in the package cannot collide with them.
var (
	progressBenchEventSink ProgressEvent
	progressBenchOKSink    bool
	buildArgsBenchSink     []string
)

// BenchmarkParseProgressLine measures the steady-state parse of one download
// progress line, exactly as emitted by downloadProgressTemplate. The parser
// must stay allocation-free: it walks the line with strings.IndexByte and
// parses each field straight from its substring, so no per-field
// strconv.ParseFloat call may allocate either.
func BenchmarkParseProgressLine(b *testing.B) {
	line := "DL|dQw4w9WgXcQ|downloading|10485760|104857600|NA|3145728|42|10.00"

	b.ReportAllocs()
	b.SetBytes(int64(len(line)))

	var ev ProgressEvent
	var ok bool
	for i := 0; i < b.N; i++ {
		ev, ok = ParseProgressLine(line)
	}
	progressBenchEventSink, progressBenchOKSink = ev, ok
}

// BenchmarkParseProgressLine_NA measures the NA-heavy worst case: every
// numeric field takes the naFloat sentinel path instead of parsing a number.
func BenchmarkParseProgressLine_NA(b *testing.B) {
	line := "DL|dQw4w9WgXcQ|downloading|NA|NA|NA|NA|NA|NA"

	b.ReportAllocs()
	b.SetBytes(int64(len(line)))

	var ev ProgressEvent
	var ok bool
	for i := 0; i < b.N; i++ {
		ev, ok = ParseProgressLine(line)
	}
	progressBenchEventSink, progressBenchOKSink = ev, ok
}

// BenchmarkBuildDownloadArgs measures argv assembly for the canonical
// download: 1080p video selected by playlist index, cookies from a browser,
// a download archive, and both binaries configured.
func BenchmarkBuildDownloadArgs(b *testing.B) {
	c := New("yt-dlp", "ffmpeg")
	opts := DownloadOpts{
		PlaylistURL: "https://www.youtube.com/playlist?list=PLbenchPLbenchPLbench",
		Video: Video{
			Index:    7,
			ID:       "dQw4w9WgXcQ",
			Title:    "Benchmark Video",
			URL:      "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			Duration: 213.0,
		},
		TargetDir:        `C:\Users\bench\Downloads\YouTube`,
		Quality:          Q1080,
		FilenameTemplate: "%(playlist_index)03d - %(title)s [%(id)s].%(ext)s",
		CookiesBrowser:   "chrome",
		ArchivePath:      `C:\Users\bench\Downloads\YouTube\.yt-dlp-archive.txt`,
	}

	b.ReportAllocs()

	var args []string
	for i := 0; i < b.N; i++ {
		args = c.BuildDownloadArgs(opts)
	}
	buildArgsBenchSink = args
}
