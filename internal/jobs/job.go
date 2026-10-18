package jobs

import (
	"strings"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// maxErrLen bounds the length of a failure message kept in a snapshot.
const maxErrLen = 200

// Status is the lifecycle state of a single download job.
type Status int

// Job states, in lifecycle order.
const (
	StatusQueued Status = iota
	StatusRunning
	StatusMerging
	StatusDone
	StatusSkipped
	StatusFailed
	StatusCanceled
)

// String returns the lowercase wire name of the status.
func (s Status) String() string {
	switch s {
	case StatusQueued:
		return "queued"
	case StatusRunning:
		return "running"
	case StatusMerging:
		return "merging"
	case StatusDone:
		return "done"
	case StatusSkipped:
		return "skipped"
	case StatusFailed:
		return "failed"
	case StatusCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// JobSnapshot is an immutable copy of one job's state.
type JobSnapshot struct {
	Video      ytdlp.Video
	Status     Status
	Percent    float64 // 0..100; -1 unknown; 100 when done
	SpeedBps   float64 // -1 unknown
	ETASeconds float64 // -1 unknown
	Err        string  // short error text when failed
}

// Summary aggregates the state of every job managed by a Manager.
type Summary struct {
	Done     int
	Skipped  int
	Failed   int
	Canceled int
	Total    int
}

// Options configures a Manager run.
type Options struct {
	PlaylistURL      string
	TargetDir        string
	Quality          ytdlp.Quality
	FilenameTemplate string
	CookiesBrowser   string
	ArchivePath      string        // "" disables archive
	Concurrency      int           // clamped 1..10
	Videos           []ytdlp.Video // pre-selected list
}

// job is the mutable state of a single queued download. Every field is
// guarded by its owning Manager's mutex.
type job struct {
	video       ytdlp.Video
	status      Status
	percent     float64
	speed       float64
	eta         float64
	errText     string
	archiveSeen bool // yt-dlp reported the video as already archived
}

// shortError flattens an error into a single bounded line for the TUI.
func shortError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if r := []rune(s); len(r) > maxErrLen {
		s = string(r[:maxErrLen])
	}
	return s
}
