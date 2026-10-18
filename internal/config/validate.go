package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// Validate normalizes the receiver in place and checks it for consistency.
// It is safe to call on a copy of a loaded configuration.
//
// The filename-template checks are deliberately OS-independent: a config
// file is portable data that may be authored on one system and loaded on
// another, so the same template must be accepted or rejected on Windows,
// Linux and macOS alike. In particular a Windows drive prefix ("C:",
// "d:") is refused on Unix too, where it would otherwise pass as an
// ordinary relative name.
func (c *Config) Validate() error {
	c.TargetDir = filepath.Clean(c.TargetDir)
	if c.TargetDir == "" || c.TargetDir == "." {
		// The zero value cleans to ".", meaning the field was never set.
		return errors.New("target folder is required")
	}

	if !strings.Contains(c.FilenameTemplate, extToken) {
		return fmt.Errorf("filename template must contain %q", extToken)
	}
	// Reject traversal and absolute templates so yt-dlp cannot write
	// outside the configured target directory.
	if hasDotDotSegment(c.FilenameTemplate) {
		return errors.New("filename template must not contain '..'")
	}
	if filepath.IsAbs(c.FilenameTemplate) || filepath.VolumeName(c.FilenameTemplate) != "" {
		return errors.New("filename template must be relative (no absolute paths)")
	}
	if strings.ContainsRune(c.FilenameTemplate, '\x00') {
		return errors.New("filename template must not contain NUL")
	}
	if len(c.FilenameTemplate) > maxFilenameTemplateLen {
		return fmt.Errorf("filename template must be at most %d characters", maxFilenameTemplateLen)
	}
	// On Windows, a rooted path without a drive volume ("/evil.%(ext)s" or
	// "\evil.%(ext)s") is not absolute, yet Python's ntpath.join (used by
	// yt-dlp) resolves it against the root of the current drive instead of
	// TargetDir.
	if hasRootedPrefix(c.FilenameTemplate) {
		return errors.New("filename template must be relative (no rooted paths)")
	}
	// Belt-and-braces: IsLocal additionally rejects templates Windows would
	// not resolve inside TargetDir (reserved device names and the like).
	// This check is host-local by nature (on Unix it only rejects rooted
	// and ".." templates); the portable drive-prefix check below covers
	// the gap it leaves on Unix.
	if !filepath.IsLocal(c.FilenameTemplate) {
		return errors.New("filename template must be relative (no rooted paths)")
	}
	// A drive prefix is portable-dangerous even though only Windows gives
	// "C:" or "d:" a special meaning: there, Python's ntpath.join (used by
	// yt-dlp) resolves both "C:\evil.%(ext)s" and the drive-relative
	// "d:rel/evil.%(ext)s" outside TargetDir. On Unix the very same config
	// would validate and silently produce a name containing ':' inside the
	// target directory, which is not the name the config author intended.
	// A shared config must behave identically everywhere, so the prefix is
	// refused on Unix and macOS as well. A colon later in the template is
	// not a drive prefix and stays allowed (yt-dlp sanitizes such names for
	// the host filesystem).
	if hasWindowsDrivePrefix(c.FilenameTemplate) {
		return errors.New("filename template must be relative (no drive prefixes)")
	}

	if c.Concurrency < minConcurrency {
		c.Concurrency = minConcurrency
	} else if c.Concurrency > maxConcurrency {
		c.Concurrency = maxConcurrency
	}

	if c.RangeStart < 0 {
		c.RangeStart = 0
	}
	if c.RangeEnd < 0 {
		c.RangeEnd = 0
	}
	if c.RangeStart > 0 && c.RangeEnd > 0 && c.RangeStart > c.RangeEnd {
		return errors.New("range start must be <= range end")
	}

	if c.Quality < ytdlp.QBest || c.Quality > ytdlp.QAudio {
		return fmt.Errorf("unsupported quality: %d", c.Quality)
	}

	switch c.CookiesBrowser {
	case "", "chrome", "chromium", "edge", "firefox", "brave", "opera", "vivaldi", "whale", "safari":
	default:
		return fmt.Errorf("unsupported browser: %s", c.CookiesBrowser)
	}

	// The playlist URL may be empty at validation time; it is validated
	// again when a fetch starts. When set, it must be an http(s) URL.
	if c.PlaylistURL != "" {
		u, err := url.Parse(c.PlaylistURL)
		if err != nil || u.Host == "" || !isHTTPScheme(u.Scheme) {
			return errors.New("invalid playlist URL")
		}
	}

	return nil
}

// hasDotDotSegment reports whether any '/'- or '\\'-separated segment of the
// filename template is ".." or Windows-normalizes to ".." by dropping trailing
// dots and spaces (".. ", "...", "....").
func hasDotDotSegment(tmpl string) bool {
	for _, seg := range strings.FieldsFunc(tmpl, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
		// Peel trailing dots and spaces, but never below the ".." prefix:
		// trimming that far would erase the very marker being detected.
		t := seg
		for len(t) > 2 && (t[len(t)-1] == '.' || t[len(t)-1] == ' ') {
			t = t[:len(t)-1]
		}
		if t == ".." {
			return true
		}
	}
	return false
}

// hasRootedPrefix reports whether the template starts with a path separator.
// On Windows such a template is rooted but has no drive volume, so it is not
// absolute, yet Python's ntpath.join (used by yt-dlp) resolves it against the
// root of the current drive instead of TargetDir.
func hasRootedPrefix(tmpl string) bool {
	return tmpl != "" && (tmpl[0] == '/' || tmpl[0] == '\\')
}

// hasWindowsDrivePrefix reports whether the template begins with a Windows
// drive prefix: an ASCII letter followed by ':' ("C:\evil.%(ext)s",
// "d:rel/evil.%(ext)s", "C:evil.%(ext)s"). The check is byte-based and
// OS-independent because filepath.VolumeName only sees the prefix on
// Windows; on Unix the same bytes pass as an ordinary relative name.
func hasWindowsDrivePrefix(tmpl string) bool {
	if len(tmpl) < 2 {
		return false
	}
	c := tmpl[0]
	if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return false
	}
	return tmpl[1] == ':'
}

// isHTTPScheme reports whether scheme is "http" or "https", ignoring case.
func isHTTPScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}
