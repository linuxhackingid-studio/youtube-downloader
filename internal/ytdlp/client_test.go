package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestExitErrorMessage pins the error text: the exit code always, and the
// final non-empty stderr line when there is one.
func TestExitErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  ExitError
		want string
	}{
		{"no stderr", ExitError{Code: 1}, "yt-dlp exited with code 1"},
		{"blank stderr", ExitError{Code: 0, Stderr: " \n\t\n"}, "yt-dlp exited with code 0"},
		{"single stderr line", ExitError{Code: 3, Stderr: "ERROR: boom\n"}, "yt-dlp exited with code 3: ERROR: boom"},
		{"last non-empty stderr line", ExitError{Code: -1, Stderr: "first\n\n  second  \n"}, "yt-dlp exited with code -1: second"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("ExitError(%+v).Error() = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// TestLastNonEmpty pins the scan for the newest non-blank line.
func TestLastNonEmpty(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"nil", nil, ""},
		{"only blanks", []string{"", "   ", "\t"}, ""},
		{"single line", []string{"one"}, "one"},
		{"last line wins", []string{"one", "two"}, "two"},
		{"trailing blanks are skipped", []string{"one", "", "   "}, "one"},
		{"lines are trimmed", []string{"one", "  two  "}, "two"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastNonEmpty(tt.lines); got != tt.want {
				t.Errorf("lastNonEmpty(%q) = %q, want %q", tt.lines, got, tt.want)
			}
		})
	}
}

// TestLastN pins the bounded tail slice.
func TestLastN(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}

	tests := []struct {
		name  string
		lines []string
		n     int
		want  []string
	}{
		{"zero keeps nothing", lines, 0, nil},
		{"negative keeps nothing", lines, -3, nil},
		{"nil input", nil, 2, nil},
		{"empty input", []string{}, 2, nil},
		{"more than available", lines, 10, lines},
		{"exactly available", lines, 4, lines},
		{"tail slice", lines, 2, []string{"c", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastN(tt.lines, tt.n); !equalStrings(got, tt.want) {
				t.Errorf("lastN(%q, %d) = %q, want %q", tt.lines, tt.n, got, tt.want)
			}
		})
	}
}

// TestExitErrorFromPlainError pins the fallbacks for failures that are not
// process exit statuses: code -1 and either the retained stderr or, when
// stderr is blank, the underlying error text.
func TestExitErrorFromPlainError(t *testing.T) {
	all := make([]string, 15)
	for i := range all {
		all[i] = fmt.Sprintf("line-%02d", i)
	}

	tests := []struct {
		name       string
		err        error
		stderr     string
		wantCode   int
		wantStderr string
	}{
		{"missing binary adopts its text", errors.New("executable file not found"), "", -1, "executable file not found"},
		{"plain error keeps stderr", errors.New("boom"), "warn line", -1, "warn line"},
		{"blank stderr falls back to the error text", errors.New("boom"), " \n\t", -1, "boom"},
		{"nil error and no stderr", nil, "", -1, ""},
		{"stderr keeps only the final ten lines", errors.New("ignored"), strings.Join(all, "\n"), -1, strings.Join(all[5:], "\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exitErrorFrom(tt.err, tt.stderr)
			if got == nil {
				t.Fatal("exitErrorFrom() = nil, want an *ExitError")
			}
			if got.Code != tt.wantCode {
				t.Errorf("exitErrorFrom().Code = %d, want %d", got.Code, tt.wantCode)
			}
			if got.Stderr != tt.wantStderr {
				t.Errorf("exitErrorFrom().Stderr = %q, want %q", got.Stderr, tt.wantStderr)
			}
		})
	}
}

// TestExitErrorFromRealExitStatus pins the exit-code path with an actual
// failed process.
func TestExitErrorFromRealExitStatus(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit 3")
	} else {
		cmd = exec.Command("sh", "-c", "exit 3")
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("running a command that exits 3 returned no error")
	}

	got := exitErrorFrom(err, "ERROR: first\nWARNING: last\n")
	if got == nil {
		t.Fatal("exitErrorFrom() = nil, want an *ExitError")
	}
	if got.Code != 3 {
		t.Errorf("exitErrorFrom().Code = %d, want 3", got.Code)
	}
	if !strings.Contains(got.Stderr, "ERROR: first") {
		t.Errorf("exitErrorFrom().Stderr = %q, want the retained stderr tail", got.Stderr)
	}
	if line := lastNonEmpty(strings.Split(got.Stderr, "\n")); line != "WARNING: last" {
		t.Errorf("last stderr line = %q, want %q", line, "WARNING: last")
	}
}

// TestVersionMissingBinary pins the not-an-exit-error path: a binary that
// cannot be launched keeps code -1 and reports the launch failure text.
func TestVersionMissingBinary(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "missing-yt-dlp"), "")

	got, err := c.Version(context.Background())
	if err == nil {
		t.Fatal("Version() = nil error for a missing binary")
	}
	if got != "" {
		t.Errorf("Version() = %q, want the empty string on error", got)
	}

	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("Version() error = %v (%T), want *ExitError", err, err)
	}
	if ee.Code != -1 {
		t.Errorf("ExitError.Code = %d, want -1 for a failed launch", ee.Code)
	}
	if strings.TrimSpace(ee.Stderr) == "" {
		t.Error("ExitError.Stderr is blank, want the launch failure text")
	}
}

// TestVersionHonorsCanceledContext pins the context-first error mapping of
// runOutput: cancellation is reported even when the command also failed to
// launch.
func TestVersionHonorsCanceledContext(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "missing-yt-dlp"), "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := c.Version(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Version(canceled ctx) error = %v, want context.Canceled", err)
	}
	if got != "" {
		t.Errorf("Version(canceled ctx) = %q, want the empty string", got)
	}
}

// TestFetchPlaylistRejectsInvalidURL pins that invalid input fails before
// any process is started.
func TestFetchPlaylistRejectsInvalidURL(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "missing-yt-dlp"), "")

	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"ftp scheme", "ftp://example.com/x"},
		{"javascript scheme", "javascript:alert(1)"},
		{"scheme-relative", "//example.com/x"},
		{"plain word", "youtube"},
		{"unparsable host whitespace", "https://exa mple.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.FetchPlaylist(context.Background(), tt.raw, "")
			if err == nil {
				t.Fatalf("FetchPlaylist(%q) = %+v, nil error; want an invalid URL error", tt.raw, got)
			}
			if got != nil {
				t.Errorf("FetchPlaylist(%q) returned a playlist alongside the error: %+v", tt.raw, got)
			}
			if !strings.Contains(err.Error(), "invalid playlist URL") {
				t.Errorf("FetchPlaylist(%q) error = %q, want an invalid playlist URL diagnostic", tt.raw, err)
			}
		})
	}
}

// TestFetchPlaylistStartFailure pins the launch-failure branch: the error is
// wrapped with the "start yt-dlp" context and is not an *ExitError.
func TestFetchPlaylistStartFailure(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "missing-yt-dlp"), "")

	got, err := c.FetchPlaylist(context.Background(), "https://example.com/playlist?list=PL1", "")
	if err == nil {
		t.Fatalf("FetchPlaylist() = %+v, nil error; want a start failure", got)
	}
	if got != nil {
		t.Errorf("FetchPlaylist() returned a playlist alongside the error: %+v", got)
	}
	if !strings.Contains(err.Error(), "start yt-dlp") {
		t.Errorf("FetchPlaylist() error = %q, want a start yt-dlp diagnostic", err)
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		t.Errorf("FetchPlaylist() error = %v, want a plain start failure rather than *ExitError", err)
	}
}

// TestStartDownloadStartFailure pins the launch-failure branch: no handle
// and a wrapped start diagnostic.
func TestStartDownloadStartFailure(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "missing-yt-dlp"), "")

	h, err := c.StartDownload(context.Background(), downloadOpts(t, "vid1"))
	if err == nil {
		t.Fatalf("StartDownload() = %+v, nil error; want a start failure", h)
	}
	if h != nil {
		t.Errorf("StartDownload() returned a handle alongside the error: %+v", h)
	}
	if !strings.Contains(err.Error(), "start yt-dlp") {
		t.Errorf("StartDownload() error = %q, want a start yt-dlp diagnostic", err)
	}
}

// TestFetchPlaylistHonorsCanceledContext pins the context-first mapping of
// the start-failure branch.
func TestFetchPlaylistHonorsCanceledContext(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "missing-yt-dlp"), "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := c.FetchPlaylist(ctx, "https://example.com/playlist?list=PL1", "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("FetchPlaylist(canceled ctx) error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Errorf("FetchPlaylist(canceled ctx) returned a playlist alongside the error: %+v", got)
	}
}
