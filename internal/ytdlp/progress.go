package ytdlp

import (
	"strconv"
	"strings"
)

// Stage identifies the phase of a download a progress event belongs to.
type Stage int

// Download stages, in lifecycle order.
const (
	StageInitializing Stage = iota
	StageDownloading
	StageMerging
	StagePostprocessing
	StageFinished
	StageError
)

// String returns the lowercase wire name of the stage.
func (s Stage) String() string {
	switch s {
	case StageInitializing:
		return "initializing"
	case StageDownloading:
		return "downloading"
	case StageMerging:
		return "merging"
	case StagePostprocessing:
		return "postprocessing"
	case StageFinished:
		return "finished"
	case StageError:
		return "error"
	default:
		return "unknown"
	}
}

// ProgressEvent is a single parsed progress update emitted by the
// downloader's yt-dlp progress template. Unknown numeric values are -1.
type ProgressEvent struct {
	VideoID    string
	Stage      Stage
	Percent    float64 // 0..100; -1 unknown
	SpeedBps   float64 // -1 unknown
	ETASeconds float64 // -1 unknown
	BytesDone  float64 // -1 unknown
	BytesTotal float64 // -1 unknown
	Line       string  // raw line
}

// ParseProgressLine parses one line of yt-dlp progress output. It accepts
// exactly two templates:
//
//	DL|<id>|<status>|<downloaded_bytes>|<total_bytes>|<total_bytes_estimate>|<speed>|<eta>|<percent>
//	PP|<id>|<status>
//
// Any other line, or a wrong field count, returns the zero event and
// false.
func ParseProgressLine(line string) (ProgressEvent, bool) {
	trimmed := strings.TrimSpace(line)
	fields := strings.Split(trimmed, "|")

	switch fields[0] {
	case "DL":
		if len(fields) != 9 {
			return ProgressEvent{}, false
		}

		total := naFloat(fields[4])
		if total < 0 {
			total = naFloat(fields[5])
		}

		percent := naFloat(fields[8])
		if percent > 100 {
			percent = 100
		}

		ev := ProgressEvent{
			VideoID:    fields[1],
			Stage:      StageDownloading,
			Percent:    percent,
			SpeedBps:   naFloat(fields[6]),
			ETASeconds: naFloat(fields[7]),
			BytesDone:  naFloat(fields[3]),
			BytesTotal: total,
			Line:       trimmed,
		}
		if fields[2] == "finished" {
			ev.Stage = StageFinished
		}
		return ev, true

	case "PP":
		if len(fields) != 3 {
			return ProgressEvent{}, false
		}

		ev := ProgressEvent{
			VideoID:    fields[1],
			Stage:      StageMerging,
			Percent:    -1,
			SpeedBps:   -1,
			ETASeconds: -1,
			BytesDone:  -1,
			BytesTotal: -1,
			Line:       trimmed,
		}
		if fields[2] == "finished" {
			ev.Stage = StageFinished
		}
		return ev, true
	}

	return ProgressEvent{}, false
}

// naFloat parses a numeric progress field, mapping empty strings, "NA",
// and malformed values to the unknown sentinel -1.
func naFloat(s string) float64 {
	if s == "" || s == "NA" {
		return -1
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return -1
	}
	return v
}
