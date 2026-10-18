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

// Field counts of the two progress templates defined in args.go. Anything
// else on the wire is not a progress line.
const (
	dlFieldCount = 9 // DL|<id>|<status>|4 numbers|<eta>|<percent>
	ppFieldCount = 3 // PP|<id>|<status>
)

// ParseProgressLine parses one line of yt-dlp progress output. It accepts
// exactly two templates:
//
//	DL|<id>|<status>|<downloaded_bytes>|<total_bytes>|<total_bytes_estimate>|<speed>|<eta>|<percent>
//	PP|<id>|<status>
//
// Any other line, or a wrong field count, returns the zero event and
// false.
//
// This is the parse hot path - a download emits up to four DL lines per
// second - so it walks the trimmed line with strings.IndexByte instead of
// strings.Split. The field slices alias the input, and strconv.ParseFloat
// reads straight from a substring, so a steady-state line costs no heap
// allocation.
func ParseProgressLine(line string) (ProgressEvent, bool) {
	trimmed := strings.TrimSpace(line)

	// One stack array serves both templates: the DL branch fills all nine
	// slots, the PP branch only the first three.
	var slots [dlFieldCount]string

	switch {
	case strings.HasPrefix(trimmed, "DL|"):
		if n := pipeFields(trimmed, slots[:]); n != dlFieldCount {
			return ProgressEvent{}, false
		}

		total := naFloat(slots[4])
		if total < 0 {
			total = naFloat(slots[5])
		}

		percent := naFloat(slots[8])
		if percent > 100 {
			percent = 100
		}

		ev := ProgressEvent{
			VideoID:    slots[1],
			Stage:      StageDownloading,
			Percent:    percent,
			SpeedBps:   naFloat(slots[6]),
			ETASeconds: naFloat(slots[7]),
			BytesDone:  naFloat(slots[3]),
			BytesTotal: total,
			Line:       trimmed,
		}
		if slots[2] == "finished" {
			ev.Stage = StageFinished
		}
		return ev, true

	case strings.HasPrefix(trimmed, "PP|"):
		if n := pipeFields(trimmed, slots[:ppFieldCount]); n != ppFieldCount {
			return ProgressEvent{}, false
		}

		ev := ProgressEvent{
			VideoID:    slots[1],
			Stage:      StageMerging,
			Percent:    -1,
			SpeedBps:   -1,
			ETASeconds: -1,
			BytesDone:  -1,
			BytesTotal: -1,
			Line:       trimmed,
		}
		if slots[2] == "finished" {
			ev.Stage = StageFinished
		}
		return ev, true
	}

	return ProgressEvent{}, false
}

// pipeFields splits s on the '|' separator shared by both progress
// templates, storing at most len(slots) fields in slots and returning how
// many fields the line holds in total. Stored values alias s and are never
// copied. Fields past the slots are counted but not stored, so a line with
// too many separators is detected without allocating for it.
func pipeFields(s string, slots []string) int {
	fields := 0
	for {
		i := strings.IndexByte(s, '|')
		if i < 0 {
			if fields < len(slots) {
				slots[fields] = s
			}
			return fields + 1
		}
		if fields < len(slots) {
			slots[fields] = s[:i]
		}
		fields++
		s = s[i+1:]
	}
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
