package config

import (
	"strings"
	"testing"
)

// TestValidateTemplateLengthBoundary pins the maxFilenameTemplateLen check
// from both sides: one byte below the cap and exactly at the cap pass, one
// byte over is refused with the documented message.
func TestValidateTemplateLengthBoundary(t *testing.T) {
	pad := func(n int) string { return strings.Repeat("a", n) }

	below := pad(maxFilenameTemplateLen-len(extToken)-1) + extToken
	at := pad(maxFilenameTemplateLen-len(extToken)) + extToken
	over := "b" + at

	if len(below) != maxFilenameTemplateLen-1 || len(at) != maxFilenameTemplateLen || len(over) != maxFilenameTemplateLen+1 {
		t.Fatalf("test setup: template lengths are %d/%d/%d, want %d/%d/%d",
			len(below), len(at), len(over),
			maxFilenameTemplateLen-1, maxFilenameTemplateLen, maxFilenameTemplateLen+1)
	}

	tests := []struct {
		name    string
		tmpl    string
		wantErr bool
	}{
		{"one byte below the cap is accepted", below, false},
		{"exactly at the cap is accepted", at, false},
		{"one byte over the cap is rejected", over, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.FilenameTemplate = tt.tmpl

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "at most 512") {
				t.Errorf("Validate() error = %q, want it to mention the 512 character limit", err)
			}
		})
	}
}

// TestValidateMinimalTemplate pins that the shortest valid template, the
// extension token alone, is accepted.
func TestValidateMinimalTemplate(t *testing.T) {
	cfg := baseValidConfig()
	cfg.FilenameTemplate = extToken

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with FilenameTemplate %q = %v, want nil", extToken, err)
	}
}

// TestValidateTemplateTrailingSeparator pins that a template ending in a
// path separator is accepted: it stays inside TargetDir and names a
// directory-like layout for yt-dlp, it does not escape the target folder.
func TestValidateTemplateTrailingSeparator(t *testing.T) {
	for _, tmpl := range []string{"sub/x.%(ext)s/", `sub\x.%(ext)s\`} {
		cfg := baseValidConfig()
		cfg.FilenameTemplate = tmpl

		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with trailing separator %q = %v, want nil", tmpl, err)
		}
	}
}

// TestValidateConcurrencyClamp pins the exact clamp result at every
// boundary of the [minConcurrency, maxConcurrency] window instead of only
// asserting "somewhere inside the window".
func TestValidateConcurrencyClamp(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{-100, minConcurrency},
		{-1, minConcurrency},
		{0, minConcurrency},
		{1, 1},
		{3, 3},
		{10, maxConcurrency},
		{11, maxConcurrency},
		{16, maxConcurrency},
		{17, maxConcurrency},
		{99, maxConcurrency},
	}

	for _, tt := range tests {
		cfg := baseValidConfig()
		cfg.Concurrency = tt.in

		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with Concurrency %d error = %v, want nil", tt.in, err)
			continue
		}
		if cfg.Concurrency != tt.want {
			t.Errorf("Validate() clamped Concurrency %d to %d, want %d", tt.in, cfg.Concurrency, tt.want)
		}
	}
}

// TestValidatePlaylistURLEdgeCases pins URL acceptance and rejection
// around whitespace padding, scheme casing, scheme lookalikes and
// malformed escapes. The empty URL stays allowed by design (it is
// re-validated when a fetch starts).
func TestValidatePlaylistURLEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"plain https", "https://www.youtube.com/playlist?list=PL1", false},
		{"plain http", "http://example.com/playlist", false},
		{"mixed case scheme", "hTtPs://example.com/pl", false},
		{"credentials in authority", "https://user:pass@example.com/pl", false},
		{"empty stays allowed", "", false},
		{"leading whitespace", " https://example.com/pl", true},
		{"trailing whitespace after the host", "https://example.com ", true},
		{"tab prefix", "\thttps://example.com/pl", true},
		{"http lookalike scheme", "httpx://example.com", true},
		{"scheme-relative", "//example.com/pl", true},
		{"hostless https", "https:///pl", true},
		{"plain word", "not-a-url", true},
		{"malformed percent escape", "https://example.com/%zz", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.PlaylistURL = tt.raw

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() with PlaylistURL %q error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}
