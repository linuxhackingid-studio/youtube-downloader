// Package tools detects the external command-line tools required by the
// downloader (yt-dlp and ffmpeg) and provides user-facing instructions
// for installing them on Windows, Linux and macOS.
package tools

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// versionTimeout bounds each individual version probe so that a hung or
// misbehaving binary can never block the caller indefinitely.
const versionTimeout = 10 * time.Second

// Status describes the detection result for a single external tool.
type Status struct {
	// Name is the tool's executable name: "yt-dlp" or "ffmpeg".
	Name string
	// Found reports whether the executable was located on PATH or in one
	// of the well-known per-OS install directories.
	Found bool
	// Path is the resolved executable path; empty when the tool is missing.
	Path string
	// Version is the parsed version string; empty when it could not be read.
	Version string
	// Err holds a one-line failure reason; empty on success.
	Err string
}

// Report is the aggregate result of checking every required tool.
type Report struct {
	YtDlp  Status
	FFmpeg Status
}

// Check detects yt-dlp and ffmpeg concurrently and reports their
// availability and versions. It never panics and always returns a fully
// populated Report, even when ctx is nil or already canceled.
func Check(ctx context.Context) Report {
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		ytdlp  Status
		ffmpeg Status
		wg     sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		ytdlp = checkTool(ctx, "yt-dlp", "--version", parseYtDlpVersion)
	}()
	go func() {
		defer wg.Done()
		ffmpeg = checkTool(ctx, "ffmpeg", "-version", parseFFmpegVersion)
	}()
	wg.Wait()

	return Report{YtDlp: ytdlp, FFmpeg: ffmpeg}
}

// checkTool probes a single tool: it resolves the executable through
// lookPath and then runs its version command under a bounded timeout.
func checkTool(ctx context.Context, name, versionArg string, parse func(string) string) Status {
	st := Status{Name: name}

	if err := ctx.Err(); err != nil {
		st.Err = err.Error()
		return st
	}

	path, err := lookPath(name)
	if err != nil {
		st.Err = name + " not found"
		return st
	}
	st.Found = true
	st.Path = path

	runCtx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(runCtx, path, versionArg)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = procAttr()

	if err := cmd.Run(); err != nil {
		st.Err = failureReason(runCtx, err, stderr.String())
		return st
	}

	st.Version = parse(stdout.String())
	if st.Version == "" {
		st.Err = "no version output"
	}
	return st
}

// failureReason converts a command failure into a compact, single-line
// reason, appending the last non-empty stderr line when one exists.
func failureReason(runCtx context.Context, err error, stderr string) string {
	reason := err.Error()
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		reason = "timed out after " + versionTimeout.String()
	case errors.Is(runCtx.Err(), context.Canceled):
		reason = "canceled"
	}
	if last := lastNonEmptyLine(stderr); last != "" {
		reason += ": " + last
	}
	return reason
}
