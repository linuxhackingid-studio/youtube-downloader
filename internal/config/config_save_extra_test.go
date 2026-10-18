package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestSaveErrorPaths drives Save through each of its failure exits: an
// unusable config directory, a temporary file that cannot be created, and a
// target path that cannot be replaced. Every case also proves the error is
// wrapped with the operation name so callers can tell the stages apart.
func TestSaveErrorPaths(t *testing.T) {
	tests := []struct {
		name         string
		arrange      func(t *testing.T, path string)
		wantSub      string
		checkTmpGone bool
	}{
		{
			name: "config directory is a regular file",
			arrange: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(filepath.Dir(path), []byte("blocker"), 0o644); err != nil {
					t.Fatalf("WriteFile(%q): %v", filepath.Dir(path), err)
				}
			},
			wantSub: "create config directory",
		},
		{
			// The staged file name is random, so the write stage is
			// forced through the createTemp seam rather than a
			// pre-created path.
			name: "temporary file cannot be created",
			arrange: func(t *testing.T, _ string) {
				t.Helper()
				blockCreateTemp(t)
			},
			wantSub: "write config",
		},
		{
			name: "target path is a directory",
			arrange: func(t *testing.T, path string) {
				t.Helper()
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatalf("MkdirAll(%q): %v", path, err)
				}
			},
			wantSub:      "replace config",
			checkTmpGone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redirectConfigDir(t)
			path := mustPath(t)
			tt.arrange(t, path)

			err := Default().Save()
			if err == nil {
				t.Fatalf("Save() = nil, want an error mentioning %q", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("Save() error = %q, want it to mention %q", err, tt.wantSub)
			}

			if tt.checkTmpGone {
				if leftovers := tempLeftovers(t, path); len(leftovers) != 0 {
					t.Errorf("temporary files left behind after a failed replace: %v", leftovers)
				}
			}
		})
	}
}

// TestSaveKeepsExistingConfigOnFailure proves a failed Save does not destroy
// the previous file: the new content is staged beside it and the rename is
// what makes it visible, so an old valid config survives an unsuccessful
// Save.
func TestSaveKeepsExistingConfigOnFailure(t *testing.T) {
	redirectConfigDir(t)
	path := mustPath(t)

	initial := Default()
	initial.Concurrency = 4
	if err := initial.Save(); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}

	// Block staging, since the temporary name can no longer be
	// pre-created: the previous config must survive untouched.
	blockCreateTemp(t)

	replacement := Default()
	replacement.Concurrency = 4
	replacement.PlaylistURL = "https://example.com/pl"
	replacement.FilenameTemplate = ""
	if err := replacement.Save(); err == nil {
		t.Fatal("Save() = nil while the temp file could not be created, want an error")
	}
	if leftovers := tempLeftovers(t, path); len(leftovers) != 0 {
		t.Errorf("temporary files left behind after a failed Save(): %v", leftovers)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() after a failed Save() error = %v", err)
	}
	if got.Concurrency != 4 {
		t.Errorf("Load().Concurrency = %d after a failed Save(), want the previous value 4", got.Concurrency)
	}
}

// TestSaveLeavesNoTempFilesAndRoundTrips covers the observable contract of
// the hardened Save: the staged "config-*.tmp" file is gone once Save
// returns, only the config file remains in the directory, the saved
// content round-trips through Load, and the file carries the restrictive
// 0600 mode on platforms that honor mode bits.
func TestSaveLeavesNoTempFilesAndRoundTrips(t *testing.T) {
	redirectConfigDir(t)
	path := mustPath(t)

	cfg := Default()
	cfg.PlaylistURL = "https://www.youtube.com/playlist?list=PLstaged"
	cfg.Concurrency = 6
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if leftovers := tempLeftovers(t, path); len(leftovers) != 0 {
		t.Errorf("Save() left temporary files behind: %v", leftovers)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", filepath.Dir(path), err)
	}
	if len(entries) != 1 || entries[0].Name() != configFileName {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("config directory holds %v, want only %q", names, configFileName)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("Load() after Save()\n got = %+v\nwant = %+v", got, cfg)
	}

	if runtime.GOOS == "windows" {
		// Windows does not model Unix permission bits; the mode check
		// below would be meaningless there.
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("saved config mode = %#o, want 0600", perm)
	}
}

// TestLoadSizeLimit pins the maxConfigBytes read cap: a file exactly at the
// limit is still parsed, a larger one is refused with an error naming the
// limit instead of being read into memory. The padding is JSON whitespace,
// so the at-limit file stays valid JSON on purpose.
func TestLoadSizeLimit(t *testing.T) {
	redirectConfigDir(t)
	path := mustPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}

	atLimit := []byte(`{"concurrency": 5}`)
	atLimit = append(atLimit, bytes.Repeat([]byte{' '}, maxConfigBytes-len(atLimit))...)
	if len(atLimit) != maxConfigBytes {
		t.Fatalf("test setup produced %d bytes, want exactly %d", len(atLimit), maxConfigBytes)
	}
	if err := os.WriteFile(path, atLimit, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v at the size limit, want nil", err)
	}
	if got.Concurrency != 5 {
		t.Errorf("Load().Concurrency = %d at the size limit, want 5", got.Concurrency)
	}

	if err := os.WriteFile(path, append(atLimit, ' '), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	_, err = Load()
	if err == nil {
		t.Fatal("Load() error = nil for a config over the size limit, want an error")
	}
	if !errors.Is(err, errConfigTooLarge) || !strings.Contains(err.Error(), "limit") {
		t.Errorf("Load() error = %q, want it to wrap errConfigTooLarge and mention the limit", err)
	}
}

// blockCreateTemp makes the staging step of Save fail for the remainder of
// the test by replacing the createTemp seam.
func blockCreateTemp(t *testing.T) {
	t.Helper()
	saved := createTemp
	createTemp = func(string, string) (*os.File, error) {
		return nil, errors.New("temp file blocked by test")
	}
	t.Cleanup(func() { createTemp = saved })
}

// tempLeftovers returns the staged "config-*.tmp" files left beside path.
func tempLeftovers(t *testing.T, path string) []string {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), "config-*.tmp"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	return leftovers
}
