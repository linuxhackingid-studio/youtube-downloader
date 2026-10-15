// Package config loads and persists the application's user settings in a
// JSON file under the operating system's user configuration directory.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

const (
	// appDirName is the per-application directory inside os.UserConfigDir.
	appDirName = "lhi-youtube-downloader"
	// configFileName is the JSON settings file name.
	configFileName = "config.json"

	// extToken must appear in every filename template so that yt-dlp can
	// pick the container extension.
	extToken = "%(ext)s"
	// defaultFilenameTemplate is the documented default output layout.
	defaultFilenameTemplate = "%(playlist_index)03d - %(title)s [%(id)s].%(ext)s"
	// maxFilenameTemplateLen bounds the template so a single field cannot
	// exceed typical filesystem path limits on its own.
	maxFilenameTemplateLen = 512

	// default/min/maxConcurrency bound parallel downloads.
	defaultConcurrency = 3
	minConcurrency     = 1
	maxConcurrency     = 10

	// fallbackTargetDir is used when the user home directory is unknown.
	fallbackTargetDir = "YouTubeDownloads"
)

// Config holds persistent application settings. It doubles as the on-disk
// JSON schema and contains no secrets.
type Config struct {
	PlaylistURL      string        `json:"playlist_url"`
	TargetDir        string        `json:"target_dir"`
	Quality          ytdlp.Quality `json:"quality"`
	FilenameTemplate string        `json:"filename_template"`
	CookiesBrowser   string        `json:"cookies_browser"` // "" = none
	Concurrency      int           `json:"concurrency"`
	RangeStart       int           `json:"range_start"` // 0 = unset (1-based inclusive)
	RangeEnd         int           `json:"range_end"`   // 0 = unset
}

// Default returns sane starting values.
func Default() Config {
	targetDir := fallbackTargetDir
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		targetDir = filepath.Join(home, "Downloads", "YouTube")
	}
	return Config{
		TargetDir:        targetDir,
		Quality:          ytdlp.QBest,
		FilenameTemplate: defaultFilenameTemplate,
		Concurrency:      defaultConcurrency,
	}
}

// Path returns the configuration file path
// (os.UserConfigDir()/lhi-youtube-downloader/config.json).
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, appDirName, configFileName), nil
}

// Load reads the configuration file. A missing, unreadable, or corrupt file
// is not an error: the caller receives the defaults. Partial JSON overrides
// the defaults field-by-field. Load does not validate; the caller decides.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Default(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// Missing or unreadable config: graceful fallback, never an error.
		return Default(), nil
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Corrupt JSON: graceful fallback, never an error.
		return Default(), nil
	}
	return cfg, nil
}

// Save writes the configuration atomically: it encodes to JSON, writes a
// temporary file next to the target, and renames it into place so readers
// never observe a partially written file.
func (c Config) Save() error {
	path, err := Path()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// Validate normalizes the receiver in place and checks it for consistency.
// It is safe to call on a copy of a loaded configuration.
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
	if !filepath.IsLocal(c.FilenameTemplate) {
		return errors.New("filename template must be relative (no rooted paths)")
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

// isHTTPScheme reports whether scheme is "http" or "https", ignoring case.
func isHTTPScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}
