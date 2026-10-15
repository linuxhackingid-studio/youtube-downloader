// Package ytdlp wraps the external yt-dlp and ffmpeg binaries: it builds
// argument lists, parses machine-readable progress output, and reports
// download errors in a form the TUI can render.
package ytdlp

import "strconv"

// Quality identifies a user-selectable output quality.
type Quality int

// Available qualities, in the exact order they are presented to users.
const (
	QBest Quality = iota
	Q2160
	Q1440
	Q1080
	Q720
	Q480
	Q360
	QAudio
)

// AllQualities returns every selectable quality in display order.
func AllQualities() []Quality {
	return []Quality{QBest, Q2160, Q1440, Q1080, Q720, Q480, Q360, QAudio}
}

// Label returns the human-readable name of the quality. Unknown values
// report "Unknown".
func (q Quality) Label() string {
	switch q {
	case QBest:
		return "Best available"
	case Q2160:
		return "2160p"
	case Q1440:
		return "1440p"
	case Q1080:
		return "1080p"
	case Q720:
		return "720p"
	case Q480:
		return "480p"
	case Q360:
		return "360p"
	case QAudio:
		return "Audio only (MP3)"
	default:
		return "Unknown"
	}
}

// String implements fmt.Stringer and is identical to Label.
func (q Quality) String() string {
	return q.Label()
}

// Height returns the numeric vertical resolution cap for video
// qualities. QBest, QAudio, and unknown values return 0.
func (q Quality) Height() int {
	switch q {
	case Q2160:
		return 2160
	case Q1440:
		return 1440
	case Q1080:
		return 1080
	case Q720:
		return 720
	case Q480:
		return 480
	case Q360:
		return 360
	default:
		return 0
	}
}

// FormatSelector returns the yt-dlp -f format expression for the quality.
// Best-quality and unknown values request the best video+audio pair,
// audio-only requests the best audio stream, and capped video qualities
// prefer a stream at or below the requested height, falling back to the
// best single file.
func (q Quality) FormatSelector() string {
	switch {
	case q == QBest:
		return "bv*+ba/b"
	case q == QAudio:
		return "ba/b"
	default:
		h := q.Height()
		if h <= 0 {
			return "bv*+ba/b"
		}
		cap := "height<=?" + strconv.Itoa(h)
		return "bv*[" + cap + "]+ba/b[" + cap + "]/b"
	}
}

// IsAudio reports whether the quality selects an audio-only download.
func (q Quality) IsAudio() bool {
	return q == QAudio
}
