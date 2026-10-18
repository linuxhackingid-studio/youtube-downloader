package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestParseFFmpegVersion pins the version-token extraction against real
// ffmpeg -version first lines, including release, git and gyan.dev builds.
func TestParseFFmpegVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "release build",
			in:   "ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers",
			want: "9.0.2",
		},
		{
			name: "git build",
			in:   "ffmpeg version N-126374-g089a48eb36-20260831 Copyright (c) 2000-2026 the FFmpeg developers",
			want: "N-126374-g089a48eb36-20260831",
		},
		{
			name: "gyan.dev full build",
			in:   "ffmpeg version 7.1.1-full_build-www.gyan.dev Copyright (c) 2000-2025",
			want: "7.1.1-full_build-www.gyan.dev",
		},
		{
			name: "empty output",
			in:   "",
			want: "",
		},
		{
			name: "single token",
			in:   "ffmpeg",
			want: "",
		},
		{
			name: "version keyword without value",
			in:   "ffmpeg version",
			want: "",
		},
		{
			name: "multi-line output uses first line",
			in:   "ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers\r\nbuilt with clang 20.1.0",
			want: "9.0.2",
		},
		{
			name: "windows crlf terminator",
			in:   "ffmpeg version N-126374-g089a48eb36-20260831 Copyright (c) 2000-2026 the FFmpeg developers\r\n",
			want: "N-126374-g089a48eb36-20260831",
		},
		{
			name: "leading whitespace",
			in:   "  ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers",
			want: "9.0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseFFmpegVersion(tt.in); got != tt.want {
				t.Errorf("parseFFmpegVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestFirstLine covers the shared line-extraction helper used by
// parseFFmpegVersion and parseYtDlpVersion.
func TestFirstLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "single line", in: "ffmpeg version 9.0.2", want: "ffmpeg version 9.0.2"},
		{name: "multi line", in: "first\nsecond\nthird", want: "first"},
		{name: "crlf", in: "first\r\nsecond", want: "first"},
		{name: "surrounding whitespace", in: "  first  \nsecond", want: "first"},
		{name: "leading newline", in: "\nsecond", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := firstLine(tt.in); got != tt.want {
				t.Errorf("firstLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestGuideFor pins the per-OS install guides so every variant is
// exercised on any machine: each one names its package manager, ends
// with both verification commands, stays ASCII-only, and keeps every
// line inside the 72 columns the bordered TUI box renders.
func TestGuideFor(t *testing.T) {
	t.Parallel()

	const maxLineWidth = 72

	tests := []struct {
		name     string
		goos     string
		mustHave []string
	}{
		{
			name: "windows installs with winget",
			goos: "windows",
			mustHave: []string{
				"winget",
				"winget install yt-dlp.yt-dlp",
				"winget install Gyan.FFmpeg",
				"winget install yt-dlp.FFmpeg",
				"press R to re-check",
			},
		},
		{
			name: "macos installs with homebrew",
			goos: "darwin",
			mustHave: []string{
				"brew install yt-dlp",
				"brew install ffmpeg",
				"yt-dlp_macos",
				"evermeet.cx",
				"press R to re-check",
			},
		},
		{
			name: "linux installs with distro packages",
			goos: "linux",
			mustHave: []string{
				"apt install yt-dlp",
				"pipx install yt-dlp",
				"dnf install ffmpeg yt-dlp",
				"pacman -S yt-dlp ffmpeg",
				"BtbN",
				"press R to re-check",
			},
		},
		{
			name:     "unknown systems fall back to linux",
			goos:     "freebsd",
			mustHave: []string{"apt install yt-dlp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			guide := guideFor(tt.goos)
			for _, want := range tt.mustHave {
				if !strings.Contains(guide, want) {
					t.Errorf("guideFor(%q) does not contain %q:\n%s", tt.goos, want, guide)
				}
			}
			for _, want := range []string{"yt-dlp --version", "ffmpeg -version"} {
				if !strings.Contains(guide, want) {
					t.Errorf("guideFor(%q) lacks the verification command %q", tt.goos, want)
				}
			}

			for i, line := range strings.Split(guide, "\n") {
				if n := utf8.RuneCountInString(line); n != len(line) {
					t.Errorf("guideFor(%q) line %d is not ASCII: %q", tt.goos, i+1, line)
				}
				if n := len(line); n > maxLineWidth {
					t.Errorf("guideFor(%q) line %d is %d columns wide (max %d): %q",
						tt.goos, i+1, n, maxLineWidth, line)
				}
			}
		})
	}
}

// TestInstallGuideMatchesRuntime documents that the exported entry point
// always serves the guide for the OS the binary was built for.
func TestInstallGuideMatchesRuntime(t *testing.T) {
	t.Parallel()

	if got, want := InstallGuide(), guideFor(runtime.GOOS); got != want {
		t.Errorf("InstallGuide() differs from guideFor(%q)", runtime.GOOS)
	}
}

// TestLookPathNotFound pins the failure mode of lookPath and the concise
// message checkTool reports for it.
func TestLookPathNotFound(t *testing.T) {
	t.Parallel()

	const name = "lhi-absent-tool-for-tests"

	if path, err := lookPath(name); err == nil {
		t.Fatalf("lookPath(%q) = %q, nil; want a not-found error", name, path)
	} else if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("lookPath(%q) error = %v, want exec.ErrNotFound", name, err)
	}

	st := checkTool(context.Background(), name, "--version", firstLine)
	if st.Found || st.Path != "" {
		t.Errorf("checkTool(%q) = %+v, want a missing tool", name, st)
	}
	if want := name + " not found"; st.Err != want {
		t.Errorf("checkTool(%q) error = %q, want %q", name, st.Err, want)
	}
}

// TestFallbackDirsInvariants checks the contract of the fallback list:
// only existing directories, no duplicates, and nothing that PATH
// already covers.
func TestFallbackDirsInvariants(t *testing.T) {
	t.Parallel()

	onPath := make(map[string]struct{})
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		onPath[dirKey(filepath.Clean(dir))] = struct{}{}
	}

	seen := make(map[string]struct{})
	for _, dir := range fallbackDirs() {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Errorf("fallbackDirs() returned %q, which is not an existing directory: %v", dir, err)
			continue
		}

		key := dirKey(filepath.Clean(dir))
		if _, dup := seen[key]; dup {
			t.Errorf("fallbackDirs() returned the duplicate %q", dir)
		}
		seen[key] = struct{}{}

		if _, covered := onPath[key]; covered {
			t.Errorf("fallbackDirs() returned %q, which is already on PATH", dir)
		}
	}
}
