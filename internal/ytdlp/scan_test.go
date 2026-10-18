package ytdlp

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// collectEvents runs scanOutput over input to completion and returns the
// events it emitted, oldest first.
func collectEvents(t *testing.T, input, videoID string, parse bool, ring *stderrRing) []ProgressEvent {
	t.Helper()

	var wg sync.WaitGroup
	wg.Add(1)
	events := make(chan ProgressEvent, 32)

	go scanOutput(strings.NewReader(input), events, videoID, parse, ring, &wg)

	done := make(chan struct{})
	var got []ProgressEvent
	go func() {
		for ev := range events {
			got = append(got, ev)
		}
		close(done)
	}()

	wg.Wait()
	close(events)
	<-done
	return got
}

// TestScanOutputParsesProgressLines pins the parse mode: progress lines
// become parsed events (keeping the raw line), blank lines are dropped,
// everything else becomes a raw initializing event, and an event with an
// empty video id inherits the one passed by the caller.
func TestScanOutputParsesProgressLines(t *testing.T) {
	input := strings.Join([]string{
		"DL|vid1|downloading|10|100|NA|5|2|50.0",
		"",
		"   ",
		"  PP|vid1|processing",
		"not a progress line",
		"DL||downloading|1|2|NA|NA|NA|7",
	}, "\n") + "\n"

	got := collectEvents(t, input, "fallback-id", true, nil)
	if len(got) != 4 {
		t.Fatalf("scanOutput emitted %d events, want 4: %+v", len(got), got)
	}

	if ev := got[0]; ev.VideoID != "vid1" || ev.Stage != StageDownloading || ev.Percent != 50 {
		t.Errorf("event 0 = %+v, want the DL line parsed as vid1 downloading at 50 percent", ev)
	}

	if ev := got[1]; ev.VideoID != "vid1" || ev.Stage != StageMerging {
		t.Errorf("event 1 = %+v, want the indented PP line parsed as merging", ev)
	} else if want := "  PP|vid1|processing"; ev.Line != want {
		t.Errorf("event 1 raw line = %q, want %q", ev.Line, want)
	}

	if ev := got[2]; ev.VideoID != "fallback-id" || ev.Stage != StageInitializing {
		t.Errorf("event 2 = %+v, want a raw event for the caller's video id", ev)
	} else if ev.Line != "not a progress line" {
		t.Errorf("event 2 raw line = %q, want the line as scanned", ev.Line)
	}

	if ev := got[3]; ev.VideoID != "fallback-id" || ev.Percent != 7 {
		t.Errorf("event 3 = %+v, want the empty wire id replaced by the fallback", ev)
	}
}

// TestScanOutputRawMode covers the stderr shape: every non-blank line is
// forwarded as a raw initializing event with unknown numerics, and the ring
// retains the trimmed lines.
func TestScanOutputRawMode(t *testing.T) {
	var ring stderrRing
	input := "  raw line  \n\nsecond\n"

	got := collectEvents(t, input, "vid", false, &ring)

	if len(got) != 2 {
		t.Fatalf("scanOutput emitted %d events, want 2: %+v", len(got), got)
	}
	for i, ev := range got {
		if ev.VideoID != "vid" || ev.Stage != StageInitializing {
			t.Errorf("event %d = %+v, want a raw event for vid", i, ev)
		}
		if ev.Percent != -1 || ev.SpeedBps != -1 || ev.ETASeconds != -1 || ev.BytesDone != -1 || ev.BytesTotal != -1 {
			t.Errorf("event %d = %+v, want every numeric field unknown", i, ev)
		}
	}
	if want := "  raw line  "; got[0].Line != want {
		t.Errorf("event 0 raw line = %q, want %q as scanned", got[0].Line, want)
	}
	if want := []string{"raw line", "second"}; !equalStrings(ring.snapshot(), want) {
		t.Errorf("ring.snapshot() = %q, want %q", ring.snapshot(), want)
	}
}

// countingReader counts the bytes read from the wrapped reader.
type countingReader struct {
	r    io.Reader
	read int64
}

// Read implements io.Reader and sums the bytes it handed out.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
}

// TestScanOutputDrainsAfterScannerOverflow pins the failure mode of an
// oversized line: the scanner gives up, no event is emitted for the bad
// token, and the remaining stream is still drained so yt-dlp can never
// block on a full pipe.
func TestScanOutputDrainsAfterScannerOverflow(t *testing.T) {
	const remainder = 200 * 1024
	input := strings.Repeat("a", scanMaxTokenSize+1) + "\n" + strings.Repeat("b", remainder)
	reader := &countingReader{r: strings.NewReader(input)}

	var wg sync.WaitGroup
	wg.Add(1)
	events := make(chan ProgressEvent, 8)
	go scanOutput(reader, events, "vid", true, nil, &wg)

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("scanOutput did not return after a scanner overflow")
	}
	close(events)

	if n := len(events); n != 0 {
		t.Errorf("scanOutput emitted %d events for an oversized line, want 0", n)
	}
	if reader.read != int64(len(input)) {
		t.Errorf("scanOutput drained %d of %d bytes after the scanner gave up", reader.read, len(input))
	}
}

// TestStderrRingKeepsTail pins the bounded stderr tail: blank lines are
// ignored, lines are trimmed, the oldest entries are dropped at
// stderrTailLines, and snapshot returns a copy.
func TestStderrRingKeepsTail(t *testing.T) {
	var ring stderrRing
	if got := ring.snapshot(); len(got) != 0 {
		t.Errorf("empty ring snapshot = %q, want empty", got)
	}

	ring.add("  first  ")
	ring.add("")
	ring.add("   ")
	ring.add("second")

	if got, want := ring.snapshot(), []string{"first", "second"}; !equalStrings(got, want) {
		t.Fatalf("ring.snapshot() = %q, want %q", got, want)
	}

	for i := 0; i < stderrTailLines+5; i++ {
		ring.add(fmt.Sprintf("line-%02d", i))
	}

	snap := ring.snapshot()
	if len(snap) != stderrTailLines {
		t.Fatalf("len(snapshot()) = %d, want %d", len(snap), stderrTailLines)
	}
	if want := "line-05"; snap[0] != want {
		t.Errorf("snapshot()[0] = %q, want %q (oldest five evicted beyond the cap)", snap[0], want)
	}
	if want := fmt.Sprintf("line-%02d", stderrTailLines+4); snap[len(snap)-1] != want {
		t.Errorf("snapshot()[last] = %q, want %q", snap[len(snap)-1], want)
	}

	snap[0] = "mutated"
	if want := "line-05"; ring.snapshot()[0] != want {
		t.Errorf("snapshot() aliases the ring: after mutating the copy, ring[0] = %q, want %q", ring.snapshot()[0], want)
	}
}

// TestRawEvent pins the sentinel shape used for non-progress output.
func TestRawEvent(t *testing.T) {
	ev := rawEvent("vid", "a diagnostic line")

	if ev.VideoID != "vid" || ev.Stage != StageInitializing || ev.Line != "a diagnostic line" {
		t.Errorf("rawEvent() = %+v, want the video id, initializing stage and raw line", ev)
	}
	if ev.Percent != -1 || ev.SpeedBps != -1 || ev.ETASeconds != -1 || ev.BytesDone != -1 || ev.BytesTotal != -1 {
		t.Errorf("rawEvent() = %+v, want every numeric field unknown (-1)", ev)
	}
}

// equalStrings reports whether two string slices hold the same values.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
