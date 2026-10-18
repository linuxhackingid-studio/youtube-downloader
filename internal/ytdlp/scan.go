package ytdlp

import (
	"bufio"
	"io"
	"strings"
	"sync"
)

// Output scanning bounds, shared by both of a download's streams. The
// scanner starts at scanInitialBuf and grows its buffer as needed, so a
// line longer than scanMaxTokenSize fails the scanner instead of growing
// the buffer without limit. Keeping the initial buffer small bounds the
// fixed per-download footprint of a concurrent batch; the scanner is
// allowed to allocate more when a single line actually needs it.
const (
	scanInitialBuf   = 16 * 1024   // initial output scanner buffer
	scanMaxTokenSize = 1024 * 1024 // largest accepted output line
	stderrTailLines  = 40          // non-empty stderr lines retained
)

// scanOutput reads r line by line and forwards every non-empty line to
// events until the stream ends. When parse is true, lines matching a
// progress template become parsed events; every other line becomes a raw
// StageInitializing event with unknown numeric fields (-1). When ring is
// non-nil, lines are also retained in it for error reporting.
func scanOutput(r io.Reader, events chan<- ProgressEvent, videoID string, parse bool, ring *stderrRing, wg *sync.WaitGroup) {
	defer wg.Done()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, scanInitialBuf), scanMaxTokenSize)

	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if ring != nil {
			ring.add(line)
		}

		if !parse {
			events <- rawEvent(videoID, line)
			continue
		}

		ev, ok := ParseProgressLine(line)
		if !ok {
			events <- rawEvent(videoID, line)
			continue
		}
		if ev.VideoID == "" {
			ev.VideoID = videoID
		}
		ev.Line = line
		events <- ev
	}

	if sc.Err() != nil {
		// The scanner gave up - for example on a line longer than the
		// buffer limit. Keep draining so yt-dlp never blocks writing to a
		// full pipe while Wait reaps it.
		_, _ = io.Copy(io.Discard, r)
	}
}

// rawEvent builds the sentinel event forwarded for output lines that are
// not machine-readable progress.
func rawEvent(videoID, line string) ProgressEvent {
	return ProgressEvent{
		VideoID:    videoID,
		Stage:      StageInitializing,
		Percent:    -1,
		SpeedBps:   -1,
		ETASeconds: -1,
		BytesDone:  -1,
		BytesTotal: -1,
		Line:       line,
	}
}

// stderrRing keeps the tail of a download's stderr so a failed run can
// report what yt-dlp complained about. A single scanner goroutine writes,
// and the wait goroutine reads once the scanners have stopped; the mutex
// makes the handoff explicit.
type stderrRing struct {
	mu    sync.Mutex
	lines []string // at most stderrTailLines entries, oldest first
}

// add appends one non-empty, trimmed line, dropping the oldest line when
// the ring is full.
func (r *stderrRing) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.lines) < stderrTailLines {
		r.lines = append(r.lines, line)
		return
	}
	copy(r.lines, r.lines[1:])
	r.lines[len(r.lines)-1] = line
}

// snapshot returns a copy of the retained lines, oldest first.
func (r *stderrRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}
