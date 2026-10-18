package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// redirectConfigDir points the OS-specific environment variables that
// os.UserConfigDir reads (%AppData% on Windows, $HOME on macOS,
// $XDG_CONFIG_HOME/$HOME on Linux and the other Unixes) at a per-test temp
// directory, so Path/Load/Save never touch the developer's real config
// directory on any OS. It must be called before any Path/Load/Save call.
// t.Setenv restores the previous values after the test. Returns the temp
// directory for path assertions.
func redirectConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		// os.UserConfigDir prefers $XDG_CONFIG_HOME when set to an
		// absolute path and falls back to $HOME/.config otherwise, so both
		// are redirected.
		t.Setenv("XDG_CONFIG_HOME", dir)
		t.Setenv("HOME", dir)
	}
	return dir
}

// configPathFor returns the config file path that Path() produces when
// os.UserConfigDir resolves to root on the current OS.
func configPathFor(root string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(root, appDirName, configFileName)
	case "darwin":
		// os.UserConfigDir appends "/Library/Application Support" to $HOME.
		return filepath.Join(root, "Library", "Application Support", appDirName, configFileName)
	default:
		return filepath.Join(root, appDirName, configFileName)
	}
}

// mustPath returns Path() or fails the test.
func mustPath(t *testing.T) string {
	t.Helper()
	path, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v, want nil", err)
	}
	return path
}

// baseValidConfig is a fully populated, valid configuration that
// individual tests mutate one field at a time.
func baseValidConfig() Config {
	return Config{
		PlaylistURL:      "https://www.youtube.com/playlist?list=PL1",
		TargetDir:        `C:\Downloads\YouTube`,
		Quality:          ytdlp.Q1080,
		FilenameTemplate: "%(playlist_index)03d - %(title)s [%(id)s].%(ext)s",
		CookiesBrowser:   "chrome",
		Concurrency:      5,
		RangeStart:       2,
		RangeEnd:         7,
	}
}

func TestPathIsUnderRedirectedConfigDir(t *testing.T) {
	dir := redirectConfigDir(t)

	got := mustPath(t)
	if want := configPathFor(dir); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}

	rel, err := filepath.Rel(dir, got)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("Path() = %q escapes redirected config dir %q", got, dir)
	}
}

func TestValidate(t *testing.T) {
	// Host-native input for the "target dir is cleaned" case, so it
	// exercises filepath.Clean on Windows, Linux and macOS alike instead of
	// relying on a hardcoded Windows path.
	targetDirInput := filepath.Join(t.TempDir(), "DL") + string(filepath.Separator)
	targetDirCleaned := filepath.Clean(targetDirInput)

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
		check   func(t *testing.T, c *Config)
	}{
		{
			name:    "valid full config",
			mutate:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "empty target dir is rejected",
			mutate:  func(c *Config) { c.TargetDir = "" },
			wantErr: true,
		},
		{
			name:    "dot target dir is rejected",
			mutate:  func(c *Config) { c.TargetDir = "." },
			wantErr: true,
		},
		{
			name:    "filename template missing ext token is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "%(title)s.mp4" },
			wantErr: true,
		},
		{
			name:    "filename template with NUL is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "%(ext)s\x00evil" },
			wantErr: true,
		},
		{
			name: "range start after range end is rejected",
			mutate: func(c *Config) {
				c.RangeStart = 9
				c.RangeEnd = 3
			},
			wantErr: true,
		},
		{
			name:    "unsupported browser is rejected",
			mutate:  func(c *Config) { c.CookiesBrowser = "internetexplorer" },
			wantErr: true,
		},
		{
			name:    "ftp playlist url is rejected",
			mutate:  func(c *Config) { c.PlaylistURL = "ftp://x" },
			wantErr: true,
		},
		{
			name:    "javascript scheme is rejected",
			mutate:  func(c *Config) { c.PlaylistURL = "javascript:alert(1)" },
			wantErr: true,
		},
		{
			name:    "playlist url without host is rejected",
			mutate:  func(c *Config) { c.PlaylistURL = "https://" },
			wantErr: true,
		},
		{
			name:    "empty playlist url is allowed",
			mutate:  func(c *Config) { c.PlaylistURL = "" },
			wantErr: false,
		},
		{
			name:    "uppercase HTTPS scheme is allowed",
			mutate:  func(c *Config) { c.PlaylistURL = "HTTPS://example.com/playlist" },
			wantErr: false,
		},
		{
			name:    "unsupported quality is rejected",
			mutate:  func(c *Config) { c.Quality = ytdlp.Quality(99) },
			wantErr: true,
		},
		{
			name:    "empty cookies browser is allowed",
			mutate:  func(c *Config) { c.CookiesBrowser = "" },
			wantErr: false,
		},
		{
			name:    "concurrency zero is clamped into [1,10]",
			mutate:  func(c *Config) { c.Concurrency = 0 },
			wantErr: false,
			check: func(t *testing.T, c *Config) {
				t.Helper()
				if c.Concurrency < 1 || c.Concurrency > 10 {
					t.Errorf("Concurrency = %d after validating 0, want clamp into [1,10]", c.Concurrency)
				}
				t.Logf("exact clamped concurrency for input 0: %d", c.Concurrency)
			},
		},
		{
			name:    "concurrency 99 is clamped to at most 10",
			mutate:  func(c *Config) { c.Concurrency = 99 },
			wantErr: false,
			check: func(t *testing.T, c *Config) {
				t.Helper()
				if c.Concurrency < 1 || c.Concurrency > 10 {
					t.Errorf("Concurrency = %d after validating 99, want clamp into [1,10]", c.Concurrency)
				}
				t.Logf("exact clamped concurrency for input 99: %d", c.Concurrency)
			},
		},
		{
			name: "negative ranges are reset to zero",
			mutate: func(c *Config) {
				c.RangeStart = -5
				c.RangeEnd = -9
			},
			wantErr: false,
			check: func(t *testing.T, c *Config) {
				t.Helper()
				if c.RangeStart != 0 {
					t.Errorf("RangeStart = %d after validating -5, want 0", c.RangeStart)
				}
				if c.RangeEnd != 0 {
					t.Errorf("RangeEnd = %d after validating -9, want 0", c.RangeEnd)
				}
			},
		},
		{
			name:    "target dir is cleaned",
			mutate:  func(c *Config) { c.TargetDir = targetDirInput },
			wantErr: false,
			check: func(t *testing.T, c *Config) {
				t.Helper()
				if c.TargetDir != targetDirCleaned {
					t.Errorf("TargetDir = %q, want cleaned value %q", c.TargetDir, targetDirCleaned)
				}
			},
		},
		{
			name:    "filename template with ../ traversal is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "../../evil.%(ext)s" },
			wantErr: true,
		},
		{
			name:    "filename template with backslash .. segment is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "sub\\..\\evil.%(ext)s" },
			wantErr: true,
		},
		{
			// "C:\evil.%(ext)s" is absolute on Windows, and on every other
			// OS it is a drive prefix, so it must be rejected everywhere.
			name:    "absolute drive filename template is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "C:\\evil.%(ext)s" },
			wantErr: true,
		},
		{
			// Lowercase drive prefix: Windows treats "d:" as a drive
			// letter, and Unix must not accept it as an ordinary relative
			// name (a config cannot behave differently per OS).
			name:    "lowercase drive prefix filename template is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "d:rel/evil.%(ext)s" },
			wantErr: true,
		},
		{
			// No separator after the colon: still a drive-relative prefix
			// on Windows and still a ':' name on Unix.
			name:    "drive prefix without trailing separator is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "C:evil.%(ext)s" },
			wantErr: true,
		},
		{
			// The colon sits at byte 1, so this is a drive prefix too; only
			// colons later in the template are left to yt-dlp's per-OS
			// sanitizing (see TestFilenameTemplateColonLater).
			name:    "letter-colon prefix filename template is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "a:b/%(ext)s" },
			wantErr: true,
		},
		{
			name:    "UNC filename template is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "\\\\server\\share\\x.%(ext)s" },
			wantErr: true,
		},
		{
			name:    "forward slash rooted filename template is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "/evil.%(ext)s" },
			wantErr: true,
		},
		{
			name:    "backslash rooted filename template is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "\\evil.%(ext)s" },
			wantErr: true,
		},
		{
			name:    "dotdot with trailing space segment is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "x/.. /y.%(ext)s" },
			wantErr: true,
		},
		{
			name:    "default filename template is accepted",
			mutate:  func(c *Config) { c.FilenameTemplate = defaultFilenameTemplate },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, &cfg)
			}
		})
	}
}

func TestDefault(t *testing.T) {
	got := Default()

	if !strings.Contains(got.FilenameTemplate, "%(ext)s") {
		t.Errorf("Default().FilenameTemplate = %q, want it to contain %q", got.FilenameTemplate, "%(ext)s")
	}
	if got.Concurrency < 1 {
		t.Errorf("Default().Concurrency = %d, want >= 1", got.Concurrency)
	}
	if got.Quality != ytdlp.QBest {
		t.Errorf("Default().Quality = %v, want %v", got.Quality, ytdlp.QBest)
	}
	if strings.TrimSpace(got.TargetDir) == "" {
		t.Errorf("Default().TargetDir is empty, want a usable download directory")
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	redirectConfigDir(t)

	path := mustPath(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config file unexpectedly exists before Load(): stat error = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for a missing file", err)
	}
	if want := Default(); !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, want defaults %+v", got, want)
	}
}

func TestLoadCorruptJSONReturnsDefaults(t *testing.T) {
	redirectConfigDir(t)

	path := mustPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(`{"target_dir": "C:\\DL", `), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for corrupt JSON", err)
	}
	if want := Default(); !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, want defaults %+v", got, want)
	}
}

func TestLoadPartialJSONOverridesDefaults(t *testing.T) {
	redirectConfigDir(t)

	path := mustPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	const partial = `{"concurrency": 7, "cookies_browser": "firefox", "target_dir": "C:\\Custom"}`
	if err := os.WriteFile(path, []byte(partial), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := Default()
	want.Concurrency = 7
	want.CookiesBrowser = "firefox"
	want.TargetDir = `C:\Custom`
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, want defaults with overrides %+v", got, want)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	redirectConfigDir(t)

	cfg := Default()
	cfg.PlaylistURL = "https://www.youtube.com/playlist?list=PLroundtrip"
	cfg.TargetDir = `J:\go\lhi-youtube-downloader\testdata\out`
	cfg.Quality = ytdlp.Q720
	cfg.FilenameTemplate = "%(playlist_index)03d - %(title)s [%(id)s].%(ext)s"
	cfg.CookiesBrowser = "firefox"
	cfg.Concurrency = 7
	cfg.RangeStart = 2
	cfg.RangeEnd = 9

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() after Save() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("Load() after Save()\n got = %+v\nwant = %+v", got, cfg)
	}
}

func TestSaveReplacesExistingFileAtomically(t *testing.T) {
	redirectConfigDir(t)

	cfg := Default()
	cfg.Concurrency = 2
	if err := cfg.Save(); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}

	cfg.Concurrency = 9
	cfg.Quality = ytdlp.QAudio
	if err := cfg.Save(); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Concurrency != 9 || got.Quality != ytdlp.QAudio {
		t.Errorf("Load() = %+v, want Concurrency 9 and Quality %v after replacement", got, ytdlp.QAudio)
	}

	// Save stages the new content under a unique "config-*.tmp" name
	// beside the target; the rename must leave nothing behind.
	if leftovers := tempLeftovers(t, mustPath(t)); len(leftovers) != 0 {
		t.Errorf("temporary files left behind after Save(): %v", leftovers)
	}
}

// TestConfigDirUnresolvable pins the graceful behavior when the OS config
// directory cannot be resolved (empty %AppData%, $HOME or $XDG_CONFIG_HOME,
// depending on the OS): Path and Save report errors, Load silently falls
// back to defaults.
func TestConfigDirUnresolvable(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", "")
	case "darwin":
		t.Setenv("HOME", "")
	default:
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "")
	}

	if _, err := Path(); err == nil {
		t.Error("Path() error = nil, want error when the config directory is unresolvable")
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil (graceful default fallback)", err)
	}
	if want := Default(); !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, want defaults %+v", got, want)
	}

	if err := Default().Save(); err == nil {
		t.Error("Save() error = nil, want error when config dir is unresolvable")
	}
}

// TestFilenameTemplateColonLater pins that the portable drive-prefix rule
// only inspects the first two bytes of the template. A colon deeper in the
// template is not a drive prefix: on Unix and macOS it validates (yt-dlp
// sanitizes such names for the host filesystem), while on Windows the
// pre-existing filepath.VolumeName/IsLocal checks keep rejecting every
// colon, so an error is expected there.
func TestFilenameTemplateColonLater(t *testing.T) {
	cfg := baseValidConfig()
	cfg.FilenameTemplate = "sub/a:b.%(ext)s"

	err := cfg.Validate()
	wantErr := runtime.GOOS == "windows"
	if (err != nil) != wantErr {
		t.Errorf("Validate() error = %v, wantErr %v (GOOS = %s)", err, wantErr, runtime.GOOS)
	}
}

// TestFilenameTemplateDrivePrefixMessage pins the portable drive-prefix error
// text on the systems where that check is reachable: on Windows the
// pre-existing absolute-path check already rejects the same templates, so
// only the rejection (not the message) is asserted there.
func TestFilenameTemplateDrivePrefixMessage(t *testing.T) {
	cfg := baseValidConfig()
	cfg.FilenameTemplate = "d:rel/evil.%(ext)s"

	err := cfg.Validate()
	if runtime.GOOS == "windows" {
		if err == nil {
			t.Fatal("Validate() error = nil, want a drive-prefix template to be rejected on Windows too")
		}
		return
	}
	const want = "filename template must be relative (no drive prefixes)"
	if err == nil || err.Error() != want {
		t.Errorf("Validate() error = %v, want %q", err, want)
	}
}

// TestHasWindowsDrivePrefix covers the byte-level boundary of the portable
// rule: an ASCII letter followed by ':' is a drive prefix, anything else is
// not (a leading digit or ':' is left to the other template checks and to
// yt-dlp).
func TestHasWindowsDrivePrefix(t *testing.T) {
	tests := []struct {
		tmpl string
		want bool
	}{
		{`C:\evil.%(ext)s`, true},
		{"c:/evil.%(ext)s", true},
		{"d:rel/evil.%(ext)s", true},
		{"C:evil.%(ext)s", true},
		{"a:b/%(ext)s", true},
		{"sub/a:b.%(ext)s", false},
		{"1:evil.%(ext)s", false},
		{":evil.%(ext)s", false},
		{"C", false},
		{"", false},
		{"%(ext)s", false},
	}
	for _, tt := range tests {
		if got := hasWindowsDrivePrefix(tt.tmpl); got != tt.want {
			t.Errorf("hasWindowsDrivePrefix(%q) = %v, want %v", tt.tmpl, got, tt.want)
		}
	}
}
