package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// downloadGrace bounds every wait for a fake download to report through Exit
// or close its Events channel. Healthy runs finish in milliseconds, so the
// bound only guards against hangs.
const downloadGrace = 20 * time.Second

// fakeSleepMillis keeps a fake tool alive far longer than any test waits.
const fakeSleepMillis = "30000"

// fakeToolSource is compiled once per test binary by TestMain. A script
// cannot stand in for yt-dlp in the download tests: Windows starts .cmd
// files through cmd.exe without escaping their arguments, and StartDownload
// always passes progress templates containing '|' and '%', which cmd.exe
// would parse as operators before the script ever runs. The compiled tool
// reads its behavior from the environment, so every test drives it
// deterministically:
//
//	FAKE_MARKER    file written before any other work, to signal "started"
//	FAKE_SLEEP_MS  delay before any output (milliseconds)
//	FAKE_STDOUT    newline separated lines written to stdout
//	FAKE_STDERR    newline separated lines written to stderr
//	FAKE_EXIT      process exit code (default 0)
const fakeToolSource = `package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if marker := os.Getenv("FAKE_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte("started\n"), 0o644)
	}
	if ms, err := strconv.Atoi(os.Getenv("FAKE_SLEEP_MS")); err == nil && ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	emit(os.Stdout, os.Getenv("FAKE_STDOUT"))
	emit(os.Stderr, os.Getenv("FAKE_STDERR"))
	code, _ := strconv.Atoi(os.Getenv("FAKE_EXIT"))
	os.Exit(code)
}

func emit(f *os.File, data string) {
	for _, line := range strings.Split(data, "\n") {
		if line == "" {
			continue
		}
		fmt.Fprintln(f, line)
	}
}
`

var (
	fakeToolPath     string // path of the compiled fake yt-dlp
	fakeToolBuildErr error  // non-nil when the fake binary could not be built
)

// TestMain builds the compiled fake yt-dlp once for the whole package. A
// build failure is recorded and skips the tests that need the binary; the
// suite must never fail because the harness itself is unavailable.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "ytdlp-faketool-*")
	if err != nil {
		fakeToolBuildErr = fmt.Errorf("create temp dir: %w", err)
	} else {
		src := filepath.Join(tmpDir, "main.go")
		if err := os.WriteFile(src, []byte(fakeToolSource), 0o644); err != nil {
			fakeToolBuildErr = fmt.Errorf("write fake tool source: %w", err)
		} else {
			out := filepath.Join(tmpDir, "fake-yt-dlp"+fakeExeSuffix())
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			buildOut, buildErr := exec.CommandContext(ctx, "go", "build", "-o", out, src).CombinedOutput()
			cancel()
			if buildErr != nil {
				fakeToolBuildErr = fmt.Errorf("go build fake tool: %w\n%s", buildErr, buildOut)
			} else {
				fakeToolPath = out
			}
		}
	}

	code := m.Run()

	if tmpDir != "" {
		_ = os.RemoveAll(tmpDir)
	}
	os.Exit(code)
}

// fakeExeSuffix returns the platform executable suffix of the compiled fake.
func fakeExeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// requireFakeTool skips the calling test when the compiled fake is missing.
func requireFakeTool(t *testing.T) string {
	t.Helper()
	if fakeToolBuildErr != nil {
		t.Skipf("compiled fake yt-dlp unavailable: %v", fakeToolBuildErr)
	}
	if fakeToolPath == "" {
		t.Skip("compiled fake yt-dlp path is unset")
	}
	return fakeToolPath
}

// writeScriptTool writes an executable script into a temp dir and returns
// its path: a .cmd batch file on Windows, a /bin/sh script elsewhere. Only
// tests whose arguments are batch-safe (no '|' or '%') may use it.
func writeScriptTool(t *testing.T, name, unixBody, windowsBody string) string {
	t.Helper()

	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".cmd")
		if err := os.WriteFile(path, []byte("@echo off\r\n"+windowsBody), 0o755); err != nil {
			t.Fatalf("write script tool: %v", err)
		}
		return path
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+unixBody), 0o755); err != nil {
		t.Fatalf("write script tool: %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod script tool: %v", err)
	}
	return path
}

// batchEchoArg escapes s so a batch file's echo prints it verbatim.
func batchEchoArg(s string) string {
	return strings.NewReplacer("^", "^^", "&", "^&", "|", "^|", "<", "^<", ">", "^>").Replace(s)
}

// echoScriptBody builds matching shell and batch bodies that print stdout
// lines, then stderr lines, then exit with code. Batch echo needs the
// metacharacters escaped; sh needs the lines quoted.
func echoScriptBody(stdout, stderr []string, code int) (unixBody, windowsBody string) {
	var unix, win strings.Builder
	for _, line := range stdout {
		fmt.Fprintf(&unix, "echo '%s'\n", line)
		fmt.Fprintf(&win, "echo %s\r\n", batchEchoArg(line))
	}
	for _, line := range stderr {
		fmt.Fprintf(&unix, "echo '%s' 1>&2\n", line)
		fmt.Fprintf(&win, "echo %s 1>&2\r\n", batchEchoArg(line))
	}
	fmt.Fprintf(&unix, "exit %d\n", code)
	fmt.Fprintf(&win, "exit /b %d\r\n", code)
	return unix.String(), win.String()
}

// drainDownloadEvents consumes h.Events until it closes and returns the
// events in order, failing the test when the channel does not close within
// downloadGrace.
func drainDownloadEvents(t *testing.T, h *Handle) []ProgressEvent {
	t.Helper()

	done := make(chan []ProgressEvent, 1)
	go func() {
		var got []ProgressEvent
		for ev := range h.Events {
			got = append(got, ev)
		}
		done <- got
	}()

	select {
	case got := <-done:
		return got
	case <-time.After(downloadGrace):
		t.Fatalf("download events channel did not close within %v", downloadGrace)
		return nil
	}
}

// awaitDownloadExit waits for the single value on h.Exit and fails the test
// when it does not arrive within downloadGrace.
func awaitDownloadExit(t *testing.T, h *Handle) error {
	t.Helper()

	select {
	case err := <-h.Exit:
		return err
	case <-time.After(downloadGrace):
		t.Fatalf("download exit was not reported within %v", downloadGrace)
		return nil
	}
}

// downloadOpts builds the options shared by the fake download tests.
func downloadOpts(t *testing.T, videoID string) DownloadOpts {
	t.Helper()
	return DownloadOpts{
		PlaylistURL:      "https://www.youtube.com/playlist?list=PL1",
		Video:            Video{Index: 1, ID: videoID},
		TargetDir:        t.TempDir(),
		Quality:          QBest,
		FilenameTemplate: "%(id)s.%(ext)s",
	}
}

// TestVersionReadsFirstLine pins the version contract end to end: only the
// first stdout line is returned and it is trimmed.
func TestVersionReadsFirstLine(t *testing.T) {
	unixBody, winBody := echoScriptBody([]string{"2024.08.06", "extra diagnostic"}, nil, 0)
	path := writeScriptTool(t, "fake-version", unixBody, winBody)

	got, err := New(path, "").Version(context.Background())
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if got != "2024.08.06" {
		t.Errorf("Version() = %q, want the first line %q", got, "2024.08.06")
	}
}

// TestFetchPlaylistPromotesSingleVideo pins the single-video path end to
// end: the URL is trimmed before validation, the JSON is decoded, and a
// zero-entry video root becomes a one-entry playlist with a canonical URL.
func TestFetchPlaylistPromotesSingleVideo(t *testing.T) {
	const videoJSON = `{"id":"abc123","title":"My Video","type":"video","url":"https://www.youtube.com/watch?v=abc123","entries":[]}`
	unixBody, winBody := echoScriptBody([]string{videoJSON}, nil, 0)
	path := writeScriptTool(t, "fake-playlist", unixBody, winBody)

	got, err := New(path, "").FetchPlaylist(context.Background(), "  https://www.youtube.com/watch?v=abc123 \n", "")
	if err != nil {
		t.Fatalf("FetchPlaylist() error = %v", err)
	}

	want := &Playlist{
		ID:    "abc123",
		Title: "My Video",
		Videos: []Video{{
			Index: 1,
			ID:    "abc123",
			Title: "My Video",
			URL:   watchURLPrefix + "abc123",
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FetchPlaylist() mismatch\n got = %+v\nwant = %+v", got, want)
	}
}

// TestFetchPlaylistDecodeError pins the success-exit, bad-JSON branch: the
// decode failure is reported as a plain error, not an *ExitError.
func TestFetchPlaylistDecodeError(t *testing.T) {
	unixBody, winBody := echoScriptBody([]string{"this is not json"}, nil, 0)
	path := writeScriptTool(t, "fake-bad-json", unixBody, winBody)

	got, err := New(path, "").FetchPlaylist(context.Background(), "https://example.com/playlist?list=PL1", "")
	if err == nil {
		t.Fatalf("FetchPlaylist() = %+v, nil error; want a decode error", got)
	}
	if got != nil {
		t.Errorf("FetchPlaylist() returned a playlist alongside the error: %+v", got)
	}
	if !strings.Contains(err.Error(), "decode playlist JSON") {
		t.Errorf("FetchPlaylist() error = %q, want a decode playlist JSON diagnostic", err)
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		t.Errorf("FetchPlaylist() error = %v, want a plain decode error rather than *ExitError", err)
	}
}

// TestFetchPlaylistExitErrorCarriesStderrTail pins the non-zero exit branch:
// the exit code and the retained stderr tail reach the caller.
func TestFetchPlaylistExitErrorCarriesStderrTail(t *testing.T) {
	unixBody, winBody := echoScriptBody(nil, []string{
		"ERROR: first failure",
		"ERROR: playlist unavailable",
	}, 3)
	path := writeScriptTool(t, "fake-playlist-fail", unixBody, winBody)

	got, err := New(path, "").FetchPlaylist(context.Background(), "https://example.com/playlist?list=PL1", "")
	if err == nil {
		t.Fatalf("FetchPlaylist() = %+v, nil error; want an exit error", got)
	}
	if got != nil {
		t.Errorf("FetchPlaylist() returned a playlist alongside the error: %+v", got)
	}

	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("FetchPlaylist() error = %v (%T), want *ExitError", err, err)
	}
	if ee.Code != 3 {
		t.Errorf("ExitError.Code = %d, want 3", ee.Code)
	}
	if !strings.Contains(ee.Stderr, "ERROR: first failure") || !strings.Contains(ee.Stderr, "ERROR: playlist unavailable") {
		t.Errorf("ExitError.Stderr = %q, want the retained stderr lines", ee.Stderr)
	}
	if msg := err.Error(); !strings.Contains(msg, "yt-dlp exited with code 3") || !strings.Contains(msg, "ERROR: playlist unavailable") {
		t.Errorf("Error() = %q, want the exit code and the final stderr line", msg)
	}
}

// TestFetchPlaylistArgumentsReachTheProcess pins the argv contract: the
// flat-playlist flags always reach yt-dlp, and --cookies-from-browser is
// appended only when a browser is configured.
func TestFetchPlaylistArgumentsReachTheProcess(t *testing.T) {
	const (
		unixBody = "echo \"$*\" 1>&2\nexit 3\n"
		winBody  = "echo %* 1>&2\r\nexit /b 3\r\n"
	)
	path := writeScriptTool(t, "fake-args", unixBody, winBody)

	tests := []struct {
		name        string
		browser     string
		wantCookies bool
	}{
		{"without browser", "", false},
		{"with browser", "chrome", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(path, "").FetchPlaylist(context.Background(), "https://example.com/playlist?list=PL1", tt.browser)

			var ee *ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("FetchPlaylist() error = %v, want *ExitError carrying the echoed argv", err)
			}
			for _, flag := range []string{"--ignore-config", "--no-warnings", "--flat-playlist", "-J"} {
				if !strings.Contains(ee.Stderr, flag) {
					t.Errorf("argv %q is missing %s", ee.Stderr, flag)
				}
			}
			hasCookies := strings.Contains(ee.Stderr, "--cookies-from-browser")
			if hasCookies != tt.wantCookies {
				t.Errorf("argv %q contains --cookies-from-browser = %v, want %v", ee.Stderr, hasCookies, tt.wantCookies)
			}
			if tt.wantCookies && !strings.Contains(ee.Stderr, "chrome") {
				t.Errorf("argv %q is missing the browser name chrome", ee.Stderr)
			}
		})
	}
}

// pollInterval is the interval between filesystem/process checks shared by
// the tests that wait for a fake tool to reach a state.
const pollInterval = 25 * time.Millisecond

// TestFetchPlaylistCanceledWhileRunning pins the cancellation branch taken
// after the process was started: the context error wins over the decode
// failure that killing the process causes. The marker file proves the fake
// tool started, so the start-failure branch cannot mask the one under test.
func TestFetchPlaylistCanceledWhileRunning(t *testing.T) {
	path := requireFakeTool(t)
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv("FAKE_MARKER", marker)
	t.Setenv("FAKE_SLEEP_MS", fakeSleepMillis)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := New(path, "").FetchPlaylist(ctx, "https://example.com/playlist?list=PL1", "")
		errCh <- err
	}()

	deadline := time.Now().Add(downloadGrace)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Skipf("fake tool did not start within %v; cannot exercise mid-fetch cancellation", downloadGrace)
		}
		time.Sleep(pollInterval)
	}

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("FetchPlaylist() error = %v, want context.Canceled", err)
		}
	case <-time.After(downloadGrace):
		t.Fatal("FetchPlaylist did not return after cancellation")
	}
}

// TestStartDownloadStreamsEventsAndSucceeds drives a full download end to
// end: progress lines become parsed events, the fallback video id fills an
// empty wire id, the wire id wins when present, and a zero exit reports nil
// through Exit.
func TestStartDownloadStreamsEventsAndSucceeds(t *testing.T) {
	path := requireFakeTool(t)
	t.Setenv("FAKE_STDOUT", strings.Join([]string{
		"DL||downloading|1048576|2097152|NA|NA|NA|50",
		"PP|vid1|processing",
		"DL|wire-id|finished|2097152|2097152|NA|0|0|100",
	}, "\n"))

	h, err := New(path, "").StartDownload(context.Background(), downloadOpts(t, "vid1"))
	if err != nil {
		t.Fatalf("StartDownload() error = %v", err)
	}
	if h == nil {
		t.Fatal("StartDownload() returned a nil handle with a nil error")
	}
	if h.Pid <= 0 {
		t.Errorf("Handle.Pid = %d, want the process id of the fake yt-dlp", h.Pid)
	}

	got := drainDownloadEvents(t, h)
	if len(got) != 3 {
		t.Fatalf("download emitted %d events, want 3: %+v", len(got), got)
	}
	if ev := got[0]; ev.VideoID != "vid1" || ev.Stage != StageDownloading || ev.Percent != 50 {
		t.Errorf("event 0 = %+v, want the empty wire id replaced by vid1 at 50 percent downloading", ev)
	}
	if ev := got[1]; ev.VideoID != "vid1" || ev.Stage != StageMerging {
		t.Errorf("event 1 = %+v, want a merging event for vid1", ev)
	}
	if ev := got[2]; ev.VideoID != "wire-id" || ev.Stage != StageFinished || ev.Percent != 100 {
		t.Errorf("event 2 = %+v, want the wire id wire-id at finished 100", ev)
	}

	if exit := awaitDownloadExit(t, h); exit != nil {
		t.Errorf("Exit = %v, want nil for a successful run", exit)
	}
}

// TestStartDownloadFailureCarriesExitCodeAndStderr pins the failed-download
// branch: stderr lines surface as raw events, the exit code is preserved,
// and Stderr holds the final diagnostic.
func TestStartDownloadFailureCarriesExitCodeAndStderr(t *testing.T) {
	path := requireFakeTool(t)
	stderrLines := []string{
		"WARNING: transient network hiccup",
		"ERROR: unable to download video data: HTTP Error 403",
	}
	t.Setenv("FAKE_STDERR", strings.Join(stderrLines, "\n"))
	t.Setenv("FAKE_EXIT", "3")

	h, err := New(path, "").StartDownload(context.Background(), downloadOpts(t, "vid1"))
	if err != nil {
		t.Fatalf("StartDownload() error = %v", err)
	}

	events := drainDownloadEvents(t, h)
	if len(events) != len(stderrLines) {
		t.Fatalf("download emitted %d events, want the %d stderr lines: %+v", len(events), len(stderrLines), events)
	}
	for i, ev := range events {
		if ev.Stage != StageInitializing {
			t.Errorf("event %d = %+v, want a raw stderr event", i, ev)
		}
	}
	if last := strings.TrimSpace(events[len(events)-1].Line); !strings.Contains(last, "HTTP Error 403") {
		t.Errorf("last stderr event line = %q, want the HTTP 403 diagnostic", last)
	}

	exit := awaitDownloadExit(t, h)
	var ee *ExitError
	if !errors.As(exit, &ee) {
		t.Fatalf("Exit = %v (%T), want *ExitError", exit, exit)
	}
	if ee.Code != 3 {
		t.Errorf("ExitError.Code = %d, want 3", ee.Code)
	}
	if ee.Stderr != "ERROR: unable to download video data: HTTP Error 403" {
		t.Errorf("ExitError.Stderr = %q, want the final stderr line", ee.Stderr)
	}
}

// TestStartDownloadContextCancelSurfacesContextError pins that canceling
// the caller's context terminates the process and surfaces the context
// error through Exit.
func TestStartDownloadContextCancelSurfacesContextError(t *testing.T) {
	path := requireFakeTool(t)
	t.Setenv("FAKE_SLEEP_MS", fakeSleepMillis)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := New(path, "").StartDownload(ctx, downloadOpts(t, "vid1"))
	if err != nil {
		t.Fatalf("StartDownload() error = %v", err)
	}
	t.Cleanup(func() { _ = h.Kill() })

	cancel()

	_ = drainDownloadEvents(t, h)
	if exit := awaitDownloadExit(t, h); !errors.Is(exit, context.Canceled) {
		t.Errorf("Exit = %v, want context.Canceled for a canceled download", exit)
	}
}

// TestHandleKillTerminatesAndIsIdempotent pins the Kill contract: it
// returns nil, is safe to call twice, and the outcome still arrives through
// Exit. The exact error is deliberately not asserted: Kill kills the tree
// before canceling the run context, so the exit may legitimately be either
// the context error or the *ExitError raised by the kill itself.
func TestHandleKillTerminatesAndIsIdempotent(t *testing.T) {
	path := requireFakeTool(t)
	t.Setenv("FAKE_SLEEP_MS", fakeSleepMillis)

	h, err := New(path, "").StartDownload(context.Background(), downloadOpts(t, "vid1"))
	if err != nil {
		t.Fatalf("StartDownload() error = %v", err)
	}
	t.Cleanup(func() { _ = h.Kill() })

	if err := h.Kill(); err != nil {
		t.Fatalf("Kill() = %v, want nil", err)
	}
	if err := h.Kill(); err != nil {
		t.Fatalf("second Kill() = %v, want nil", err)
	}

	_ = drainDownloadEvents(t, h)
	if exit := awaitDownloadExit(t, h); exit == nil {
		t.Error("Exit = nil after Kill, want a non-nil error (the context error or an *ExitError)")
	}
}

// TestHandleKillNilReceiver pins the defensive nil check.
func TestHandleKillNilReceiver(t *testing.T) {
	var h *Handle
	if err := h.Kill(); err != nil {
		t.Errorf("(*Handle)(nil).Kill() = %v, want nil", err)
	}
}
