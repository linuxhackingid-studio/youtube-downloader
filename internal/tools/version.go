package tools

import "strings"

// parseYtDlpVersion extracts the version from yt-dlp --version output.
func parseYtDlpVersion(out string) string {
	return firstLine(out)
}

// parseFFmpegVersion extracts the version token from the first line of
// ffmpeg -version output. Real-world first lines look like:
//
//	ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers
//	ffmpeg version N-126374-g089a48eb36-20260831 Copyright (c) 2000-2026 the FFmpeg developers
//
// which yield "9.0.2" and "N-126374-g089a48eb36-20260831" respectively.
// Inputs that carry the "version" keyword without a value (e.g. simply
// "ffmpeg version") return "".
func parseFFmpegVersion(out string) string {
	fields := strings.Fields(firstLine(out))
	if len(fields) >= 2 && strings.EqualFold(fields[1], "version") {
		if len(fields) >= 3 {
			return fields[2]
		}
		return ""
	}
	if len(fields) >= 2 {
		return fields[1]
	}
	return ""
}

// firstLine returns the first line of s with surrounding whitespace,
// including a trailing carriage return, removed.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// lastNonEmptyLine returns the last non-blank line of s, trimmed.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
