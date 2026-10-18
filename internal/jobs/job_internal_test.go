package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestStatusString pins the lowercase wire names of every lifecycle state,
// including out-of-range values, which must never panic or come back empty.
func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusQueued, "queued"},
		{StatusRunning, "running"},
		{StatusMerging, "merging"},
		{StatusDone, "done"},
		{StatusSkipped, "skipped"},
		{StatusFailed, "failed"},
		{StatusCanceled, "canceled"},
		{Status(99), "unknown"},
		{Status(-1), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", int(tt.status), got, tt.want)
		}
	}
}

// TestShortErrorBoundsAndTrims pins shortError: nil is empty, surrounding
// whitespace is trimmed, and a message longer than maxErrLen runes is cut
// at a rune boundary, never splitting a multi-byte character.
func TestShortErrorBoundsAndTrims(t *testing.T) {
	if got := shortError(nil); got != "" {
		t.Errorf("shortError(nil) = %q, want empty", got)
	}
	if got := shortError(fmt.Errorf("  boom  ")); got != "boom" {
		t.Errorf("shortError(padded) = %q, want %q", got, "boom")
	}

	exact := strings.Repeat("x", maxErrLen)
	if got := shortError(fmt.Errorf("%s", exact)); got != exact {
		t.Errorf("shortError(%d ASCII chars) length = %d, want %d", maxErrLen, len(got), maxErrLen)
	}

	long := strings.Repeat("x", maxErrLen+1)
	if got := shortError(fmt.Errorf("%s", long)); got != exact {
		t.Errorf("shortError(%d ASCII chars) = %d chars, want the first %d", maxErrLen+1, len(got), maxErrLen)
	}

	multibyte := strings.Repeat("é", maxErrLen+10)
	got := shortError(fmt.Errorf("%s", multibyte))
	if n := utf8.RuneCountInString(got); n != maxErrLen {
		t.Errorf("shortError(multibyte) rune count = %d, want %d", n, maxErrLen)
	}
	if !utf8.ValidString(got) {
		t.Errorf("shortError(multibyte) produced invalid UTF-8: %q", got)
	}
}

// TestAppendLogCapsAndEvicts checks the ring behavior of appendLog: at most
// maxLogLines entries are kept, the oldest are evicted first, and every
// append bumps the revision counter.
func TestAppendLogCapsAndEvicts(t *testing.T) {
	m := NewManager(nil, Options{})

	total := maxLogLines + 1
	for i := 0; i < total; i++ {
		m.appendLog(fmt.Sprintf("line-%03d", i))
	}

	m.mu.Lock()
	got := append([]string(nil), m.logs...)
	rev := m.rev
	m.mu.Unlock()

	if len(got) != maxLogLines {
		t.Fatalf("len(logs) = %d after %d appends, want %d", len(got), total, maxLogLines)
	}
	if want := "line-001"; got[0] != want {
		t.Errorf("logs[0] = %q, want %q (oldest entry evicted)", got[0], want)
	}
	if want := fmt.Sprintf("line-%03d", total-1); got[len(got)-1] != want {
		t.Errorf("logs[last] = %q, want %q", got[len(got)-1], want)
	}
	if rev != uint64(total) {
		t.Errorf("rev = %d after %d appends, want %d", rev, total, total)
	}
}

// TestAppendLogCapsLineBytes pins the per-line byte cap: a line exactly at
// maxLogLineLen is stored unchanged, and a longer line is cut at the cap
// with any partial UTF-8 sequence dropped.
func TestAppendLogCapsLineBytes(t *testing.T) {
	m := NewManager(nil, Options{})

	exact := strings.Repeat("a", maxLogLineLen)
	m.appendLog(exact)

	// The euro sign is three bytes; cut at byte maxLogLineLen splits it.
	long := strings.Repeat("a", maxLogLineLen-2) + "€" + strings.Repeat("b", 16)
	m.appendLog(long)

	logs := m.Logs()
	if len(logs) != 2 {
		t.Fatalf("len(Logs()) = %d, want 2", len(logs))
	}
	if logs[0] != exact {
		t.Errorf("stored exact-length line differs: got %d bytes, want %d", len(logs[0]), len(exact))
	}
	if len(logs[1]) > maxLogLineLen {
		t.Errorf("stored line is %d bytes, want at most %d", len(logs[1]), maxLogLineLen)
	}
	if !utf8.ValidString(logs[1]) {
		t.Errorf("stored line is not valid UTF-8: %q", logs[1])
	}
	if want := strings.Repeat("a", maxLogLineLen-2); logs[1] != want {
		t.Errorf("stored line = %q, want the partial euro sign dropped (%d 'a' bytes)", logs[1], len(want))
	}
}

// TestReadArchiveParsing covers the archive parser beyond the happy path:
// CRLF and blank lines, lower/upper case prefixes, tabs and repeated
// spaces, malformed lines, comment-looking lines, foreign extractors, extra
// trailing fields, and duplicate ids.
func TestReadArchiveParsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.txt")

	const content = "youtube id1\r\n" +
		"YOUTUBE id2\n" +
		"  youtube\tid3\n" +
		"youtube   id4\n" +
		"\n" +
		"   \n" +
		"youtube\n" +
		"youtube id5 extra-fields\n" +
		"bilibili id6\n" +
		"# youtube id7\n" +
		"youtube id8\n" +
		"youtube id1\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}

	ids := readArchive(path)

	for _, want := range []string{"id1", "id2", "id3", "id4", "id5", "id8"} {
		if !ids[want] {
			t.Errorf("readArchive() is missing %q; parsed ids: %v", want, ids)
		}
	}
	for _, absent := range []string{"id6", "id7"} {
		if ids[absent] {
			t.Errorf("readArchive() unexpectedly contains %q; parsed ids: %v", absent, ids)
		}
	}
	if len(ids) != 6 {
		t.Errorf("readArchive() parsed %d ids, want 6: %v", len(ids), ids)
	}
}

// TestReadArchiveMissingFileReturnsEmptySet pins the graceful contract: a
// missing or unreadable archive yields a usable, non-nil empty set.
func TestReadArchiveMissingFileReturnsEmptySet(t *testing.T) {
	ids := readArchive(filepath.Join(t.TempDir(), "does-not-exist.txt"))
	if ids == nil {
		t.Fatal("readArchive() = nil map, want a non-nil empty set")
	}
	if len(ids) != 0 {
		t.Errorf("readArchive(missing) = %v, want empty", ids)
	}
}
