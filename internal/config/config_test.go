package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// redirectConfigDir points os.UserConfigDir (which reads %AppData% on
// Windows) at a per-test temp directory. It must be called before any
// Path/Load/Save call. Returns the temp directory for path assertions.
func redirectConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	return dir
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
	want := filepath.Join(dir, "lhi-youtube-downloader", "config.json")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}

	rel, err := filepath.Rel(dir, got)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("Path() = %q escapes redirected config dir %q", got, dir)
	}
}

func TestValidate(t *testing.T) {
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
			mutate:  func(c *Config) { c.TargetDir = `C:\DL\` },
			wantErr: false,
			check: func(t *testing.T, c *Config) {
				t.Helper()
				if c.TargetDir != `C:\DL` {
					t.Errorf("TargetDir = %q, want cleaned value %q", c.TargetDir, `C:\DL`)
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
			name:    "absolute drive filename template is rejected",
			mutate:  func(c *Config) { c.FilenameTemplate = "C:\\evil.%(ext)s" },
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

	if _, err := os.Stat(mustPath(t) + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind after Save(): stat error = %v", err)
	}
}

// TestConfigDirUnresolvable pins the graceful behavior when %AppData% is
// empty: Path and Save report errors, Load silently falls back to defaults.
func TestConfigDirUnresolvable(t *testing.T) {
	t.Setenv("AppData", "")

	if _, err := Path(); err == nil {
		t.Error("Path() error = nil, want error when %AppData% is empty")
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
