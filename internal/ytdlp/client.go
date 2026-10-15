package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Client invokes the configured yt-dlp binary and, where post-processing
// needs it, the configured ffmpeg binary.
type Client struct {
	Path       string
	FFmpegPath string
}

// New returns a Client bound to the given yt-dlp and ffmpeg paths.
// ffmpegPath may be empty when the caller never requests merged output.
func New(path, ffmpegPath string) *Client {
	return &Client{Path: path, FFmpegPath: ffmpegPath}
}

// ExitError reports a yt-dlp process that terminated unsuccessfully. Code
// is the process exit code (-1 when no exit status was available) and
// Stderr holds the tail of the process's standard error, which is where
// yt-dlp writes its diagnostics.
type ExitError struct {
	Code   int
	Stderr string
}

// Error implements the error interface. It reports the exit code and the
// last non-empty stderr line, when there is one.
func (e *ExitError) Error() string {
	line := lastNonEmpty(strings.Split(e.Stderr, "\n"))
	if line == "" {
		return fmt.Sprintf("yt-dlp exited with code %d", e.Code)
	}
	return fmt.Sprintf("yt-dlp exited with code %d: %s", e.Code, line)
}

// Version returns the version string reported by "yt-dlp --version".
func (c *Client) Version(ctx context.Context) (string, error) {
	out, err := runOutput(ctx, c.Path, "--version")
	if err != nil {
		return "", err
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return strings.TrimSpace(out), nil
}

// runOutput runs bin with args and returns trimmed stdout. Captures
// stderr. Hidden window. Timeout left to ctx.
func runOutput(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = procAttr()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", exitErrorFrom(err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// exitErrorFrom converts a failed command run into an *ExitError that
// carries the exit code and the final ten stderr lines. Failures that are
// not an *exec.ExitError (for example a missing binary) keep code -1 and
// fall back to the underlying error text as the diagnostic.
func exitErrorFrom(err error, stderr string) *ExitError {
	exitErr := &ExitError{
		Code:   -1,
		Stderr: strings.Join(lastN(strings.Split(stderr, "\n"), 10), "\n"),
	}

	var execErr *exec.ExitError
	if errors.As(err, &execErr) {
		exitErr.Code = execErr.ExitCode()
		return exitErr
	}

	if err != nil && strings.TrimSpace(exitErr.Stderr) == "" {
		exitErr.Stderr = err.Error()
	}
	return exitErr
}

// lastNonEmpty returns the last line that is not empty after trimming
// whitespace, or the empty string when there is none.
func lastNonEmpty(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// lastN returns at most the final n entries of lines. It returns nil when
// n is not positive.
func lastN(lines []string, n int) []string {
	if n <= 0 || len(lines) == 0 {
		return nil
	}
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}
