package tools

import "testing"

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
