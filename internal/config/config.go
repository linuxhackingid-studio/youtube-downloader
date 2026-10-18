// Package config loads and persists the application's user settings in a
// JSON file under the operating system's user configuration directory.
//
// A configuration file is portable: validation rules are OS-independent so
// that a config authored on Windows, Linux or macOS is accepted or rejected
// identically by every build (see Validate).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

	// maxConfigBytes caps how much of the configuration file is read. The
	// real file is a few hundred bytes; the cap keeps a hostile or
	// accidentally huge file from being slurped into memory.
	maxConfigBytes = 1 << 20 // 1 MiB

	// tempFilePattern names the staged file Save writes before the atomic
	// rename. The "*" is replaced with a random suffix so the name cannot
	// be predicted or pre-created by another process.
	tempFilePattern = "config-*.tmp"

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

// errConfigTooLarge reports a configuration file that is larger than
// maxConfigBytes. Load surfaces it to the caller, unlike other read errors,
// which fall back to the defaults.
var errConfigTooLarge = errors.New("config file exceeds the size limit")

// Load reads the configuration file. A missing, unreadable, or corrupt file
// is not an error: the caller receives the defaults. Partial JSON overrides
// the defaults field-by-field. Load does not validate; the caller decides.
//
// At most maxConfigBytes are read; a larger file is refused with an error
// instead of being read into memory, since no valid configuration comes
// anywhere near that size.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Default(), nil
	}
	data, err := readConfigFile(path)
	if err != nil {
		if errors.Is(err, errConfigTooLarge) {
			return Default(), err
		}
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

// readConfigFile returns the configuration file contents, reading at most
// maxConfigBytes. Files over the cap fail with an error naming the limit.
func readConfigFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("%w: %q is larger than %d bytes", errConfigTooLarge, path, maxConfigBytes)
	}
	return data, nil
}

// createTemp is the temporary-file constructor used by Save. It is a
// package variable so tests can drive the write-stage failure paths, which
// are otherwise hard to reach portably.
var createTemp = os.CreateTemp

// Save writes the configuration atomically: it encodes to JSON, stages the
// content in a uniquely named temporary file beside the target, and renames
// it into place so readers never observe a partially written file.
//
// The staged file is created with os.CreateTemp, which excludes a
// pre-existing name and makes the path unpredictable, so a symlink planted
// at a guessable temporary path cannot redirect the write. It carries 0600
// permissions, which survives the rename and keeps the saved file private
// on platforms that honor mode bits. On any failure the staged file is
// removed and Save reports the stage that failed.
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

	tmp, err := stageConfig(filepath.Dir(path), data)
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// stageConfig writes data to a fresh temporary file inside dir and returns
// its path. The temporary file is flushed to stable storage before it is
// closed, so the data is durable before Save renames it into place. Any
// failure removes the partial file and returns the underlying error.
func stageConfig(dir string, data []byte) (string, error) {
	f, err := createTemp(dir, tempFilePattern)
	if err != nil {
		return "", err
	}
	name := f.Name()

	// f.Chmod is a documented no-op for permission bits Windows does not
	// model, so this only restricts access where the OS enforces modes.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}
