package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// toolsPrependPATH puts dir at the front of PATH for the duration of the
// test, so lookPath resolves the fake tools written by these tests.
func toolsPrependPATH(t *testing.T, dir string) {
	t.Helper()

	current, ok := os.LookupEnv("PATH")
	if !ok {
		t.Skip("PATH is not set")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+current)
}

// batchEscape escapes the characters cmd.exe would otherwise interpret in an
// echoed line. Percent signs are rejected by writeEchoTool instead, because
// they need doubling inside a batch file.
func batchEscape(line string) string {
	var out strings.Builder
	for _, r := range line {
		if strings.ContainsRune("^&|<>()", r) {
			out.WriteRune('^')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// writeExecutable writes content to dir/name (with a .cmd suffix on
// Windows) and returns the resulting path.
func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake tool %s: %v", path, err)
	}
	return path
}

// writeEchoTool writes a deterministic stand-in executable that echoes the
// given stdout and stderr lines and exits with exitCode.
func writeEchoTool(t *testing.T, dir, name string, stdout, stderr []string, exitCode int) string {
	t.Helper()

	var body strings.Builder
	if runtime.GOOS == "windows" {
		for _, line := range stdout {
			if strings.ContainsRune(line, '%') {
				t.Fatalf("fake tool stdout line %q contains %%, which cannot be echoed from a batch file", line)
			}
			body.WriteString("echo " + batchEscape(line) + "\r\n")
		}
		for _, line := range stderr {
			if strings.ContainsRune(line, '%') {
				t.Fatalf("fake tool stderr line %q contains %%, which cannot be echoed from a batch file", line)
			}
			body.WriteString("echo " + batchEscape(line) + " 1>&2\r\n")
		}
		body.WriteString("exit /b " + strconv.Itoa(exitCode) + "\r\n")
		return writeExecutable(t, dir, name, "@echo off\r\n"+body.String())
	}

	for _, line := range stdout {
		body.WriteString("echo '" + line + "'\n")
	}
	for _, line := range stderr {
		body.WriteString("echo '" + line + "' >&2\n")
	}
	body.WriteString("exit " + strconv.Itoa(exitCode) + "\n")
	return writeExecutable(t, dir, name, "#!/bin/sh\n"+body.String())
}

// TestParseYtDlpVersion pins the whole-output contract: only the first line
// is used, trimmed, with later diagnostics ignored.
func TestParseYtDlpVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "2026.01.01\n", want: "2026.01.01"},
		{name: "crlf", in: "2026.01.01\r\n", want: "2026.01.01"},
		{name: "no trailing newline", in: "2026.01.01", want: "2026.01.01"},
		{name: "extra output below", in: "2026.01.01\n[debug] loaded config\n", want: "2026.01.01"},
		{name: "leading whitespace", in: "  2026.01.01", want: "2026.01.01"},
		{name: "empty", in: "", want: ""},
		{name: "blank first line", in: "\n2026.01.01", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseYtDlpVersion(tt.in); got != tt.want {
				t.Errorf("parseYtDlpVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestLastNonEmptyLine pins the stderr-tail helper used by failureReason.
func TestLastNonEmptyLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "blank only", in: "\n  \n\t\n", want: ""},
		{name: "single line", in: "only", want: "only"},
		{name: "trailing blanks", in: "first\nsecond\n\n", want: "second"},
		{name: "crlf", in: "first\r\nsecond\r\n", want: "second"},
		{name: "surrounding whitespace", in: "a\n   spaced   ", want: "spaced"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastNonEmptyLine(tt.in); got != tt.want {
				t.Errorf("lastNonEmptyLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestParseFFmpegVersionFallbackToken covers the branch where the second
// field is not the "version" keyword: the second token is taken as-is.
func TestParseFFmpegVersionFallbackToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "no version keyword", in: "ffmpeg 9.0.2", want: "9.0.2"},
		{name: "no version keyword with suffix", in: "ffmpeg N-126374-g089a48eb36", want: "N-126374-g089a48eb36"},
		{name: "single token", in: "ffmpeg", want: ""},
		{name: "empty", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseFFmpegVersion(tt.in); got != tt.want {
				t.Errorf("parseFFmpegVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestFailureReason pins the compact reason text: the process error by
// default, the deadline or cancellation wording for a timed-out run, and
// the last non-empty stderr line as an appended diagnostic.
func TestFailureReason(t *testing.T) {
	const stderr = "line one\n\nWARNING: disk full\n"
	err := errors.New("exit status 3")

	t.Run("plain error with stderr tail", func(t *testing.T) {
		if got, want := failureReason(context.Background(), err, stderr), "exit status 3: WARNING: disk full"; got != want {
			t.Errorf("failureReason() = %q, want %q", got, want)
		}
	})

	t.Run("blank stderr adds nothing", func(t *testing.T) {
		if got, want := failureReason(context.Background(), err, "\n \n"), "exit status 3"; got != want {
			t.Errorf("failureReason() = %q, want %q", got, want)
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		want := "canceled: WARNING: disk full"
		if got := failureReason(ctx, errors.New("signal: killed"), stderr); got != want {
			t.Errorf("failureReason() = %q, want %q", got, want)
		}
	})

	t.Run("deadline exceeded", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		<-ctx.Done()

		want := "timed out after " + versionTimeout.String() + ": WARNING: disk full"
		if got := failureReason(ctx, errors.New("signal: killed"), stderr); got != want {
			t.Errorf("failureReason() = %q, want %q", got, want)
		}
	})
}

// TestCheckToolSuccess drives the full checkTool path against a fake
// executable: it resolves the tool, runs the version command and parses the
// first line.
func TestCheckToolSuccess(t *testing.T) {
	dir := t.TempDir()
	name := "lhi-faketool-check-ok"
	writeEchoTool(t, dir, name, []string{"2026.01.01"}, nil, 0)
	toolsPrependPATH(t, dir)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	st := checkTool(context.Background(), name, "--version", parseYtDlpVersion)
	if !st.Found {
		t.Fatalf("checkTool(%q) Found = false, want true (status %+v)", name, st)
	}
	if st.Name != name {
		t.Errorf("checkTool(%q).Name = %q, want %q", name, st.Name, name)
	}
	if st.Path == "" {
		t.Errorf("checkTool(%q).Path is empty, want the resolved executable", name)
	}
	if st.Version != "2026.01.01" {
		t.Errorf("checkTool(%q).Version = %q, want %q", name, st.Version, "2026.01.01")
	}
	if st.Err != "" {
		t.Errorf("checkTool(%q).Err = %q, want empty on success", name, st.Err)
	}
}

// TestCheckToolFailure pins the failure reason for a non-zero exit,
// including the last stderr line.
func TestCheckToolFailure(t *testing.T) {
	dir := t.TempDir()
	name := "lhi-faketool-check-fail"
	writeEchoTool(t, dir, name, nil, []string{"fake failure line"}, 3)
	toolsPrependPATH(t, dir)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	st := checkTool(context.Background(), name, "--version", parseYtDlpVersion)
	if !st.Found {
		t.Fatalf("checkTool(%q) Found = false, want true (status %+v)", name, st)
	}
	if st.Version != "" {
		t.Errorf("checkTool(%q).Version = %q, want empty after a failure", name, st.Version)
	}
	if !strings.Contains(st.Err, "exit status 3") {
		t.Errorf("checkTool(%q).Err = %q, want it to mention the exit status", name, st.Err)
	}
	if !strings.Contains(st.Err, "fake failure line") {
		t.Errorf("checkTool(%q).Err = %q, want it to include the stderr tail", name, st.Err)
	}
}

// TestCheckToolWithoutVersionOutput pins the empty-output contract: a tool
// that exits cleanly but prints nothing is reported as such.
func TestCheckToolWithoutVersionOutput(t *testing.T) {
	dir := t.TempDir()
	name := "lhi-faketool-check-silent"
	writeEchoTool(t, dir, name, nil, nil, 0)
	toolsPrependPATH(t, dir)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	st := checkTool(context.Background(), name, "--version", parseYtDlpVersion)
	if !st.Found || st.Version != "" {
		t.Fatalf("checkTool(%q) = %+v, want found with an empty version", name, st)
	}
	if st.Err != "no version output" {
		t.Errorf("checkTool(%q).Err = %q, want %q", name, st.Err, "no version output")
	}
}

// TestCheckWithCanceledContext pins the graceful contract of Check: a
// canceled context is reported per tool instead of panicking or hanging,
// and a nil context is replaced by the background context.
func TestCheckWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := Check(ctx)

	statuses := map[string]Status{"yt-dlp": report.YtDlp, "ffmpeg": report.FFmpeg}
	for name, st := range statuses {
		if st.Name != name {
			t.Errorf("Check(canceled).%s.Name = %q, want %q", name, st.Name, name)
		}
		if st.Found || st.Path != "" || st.Version != "" {
			t.Errorf("Check(canceled).%s = %+v, want an unfound tool", name, st)
		}
		if st.Err != context.Canceled.Error() {
			t.Errorf("Check(canceled).%s.Err = %q, want %q", name, st.Err, context.Canceled.Error())
		}
	}
}

// TestCheckReportsMissingTools hides every search location so the test does
// not depend on what the developer machine has installed, then cares that a
// nil context is accepted and both tools are reported as missing rather
// than as an error.
func TestCheckReportsMissingTools(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", filepath.Join(empty, "no-such-entry"))

	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", filepath.Join(empty, "local"))
		t.Setenv("APPDATA", filepath.Join(empty, "roaming"))
		t.Setenv("USERPROFILE", filepath.Join(empty, "profile"))
		t.Setenv("ProgramData", filepath.Join(empty, "programdata"))
	default:
		t.Setenv("HOME", filepath.Join(empty, "home"))
	}

	if path, err := lookPath("yt-dlp"); err == nil {
		t.Skipf("this host still resolves yt-dlp at %s, which the OS fallback list makes unavoidable here", path)
	}

	report := Check(nil)

	if report.YtDlp.Err != "yt-dlp not found" {
		t.Errorf("Check(nil).YtDlp.Err = %q, want %q", report.YtDlp.Err, "yt-dlp not found")
	}
	if report.FFmpeg.Err != "ffmpeg not found" {
		t.Errorf("Check(nil).FFmpeg.Err = %q, want %q", report.FFmpeg.Err, "ffmpeg not found")
	}
	if report.YtDlp.Found || report.FFmpeg.Found {
		t.Errorf("Check(nil) = %+v, want both tools missing", report)
	}
}

// TestLookPathRejectsEmptyName pins the degenerate input guard.
func TestLookPathRejectsEmptyName(t *testing.T) {
	path, err := lookPath("")
	if path != "" {
		t.Errorf("lookPath(\"\") = %q, want empty path", path)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("lookPath(\"\") error = %v, want exec.ErrNotFound", err)
	}
}

// TestLookPathRefusesPathRelativeHit pins the ErrDot contract: a tool that
// is only reachable as ./name through a PATH entry of "." is refused, never
// silently executed from the current directory.
func TestLookPathRefusesPathRelativeHit(t *testing.T) {
	dir := t.TempDir()
	name := "lhi-faketool-dot"
	writeEchoTool(t, dir, name, nil, nil, 0)

	t.Chdir(dir)
	t.Setenv("PATH", ".")
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	path, err := lookPath(name)
	if err == nil {
		t.Fatalf("lookPath(%q) = %q with PATH=., want the ErrDot refusal", name, path)
	}
	if !errors.Is(err, exec.ErrDot) {
		t.Errorf("lookPath(%q) error = %v, want exec.ErrDot", name, err)
	}
}
