package ytdlp

import "testing"

// TestAllQualitiesOrder pins the exact user-facing ordering of the quality
// list. The TUI renders AllQualities() as-is, so the sequence is part of
// the module's contract.
func TestAllQualitiesOrder(t *testing.T) {
	want := []Quality{QBest, Q2160, Q1440, Q1080, Q720, Q480, Q360, QAudio}
	got := AllQualities()

	if len(got) != len(want) {
		t.Fatalf("AllQualities() returned %d qualities, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllQualities()[%d] = %v (%q), want %v (%q)",
				i, got[i], got[i].Label(), want[i], want[i].Label())
		}
	}
}

func TestQualityLabel(t *testing.T) {
	tests := []struct {
		name string
		q    Quality
		want string
	}{
		{"QBest", QBest, "Best available"},
		{"Q2160", Q2160, "2160p"},
		{"Q1440", Q1440, "1440p"},
		{"Q1080", Q1080, "1080p"},
		{"Q720", Q720, "720p"},
		{"Q480", Q480, "480p"},
		{"Q360", Q360, "360p"},
		{"QAudio", QAudio, "Audio only (MP3)"},
		{"invalid 99", Quality(99), "Unknown"},
		{"invalid -1", Quality(-1), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.q.Label(); got != tt.want {
				t.Errorf("Quality(%d).Label() = %q, want %q", int(tt.q), got, tt.want)
			}
		})
	}
}

// TestQualityStringMatchesLabel verifies the fmt.Stringer implementation
// stays in lockstep with Label.
func TestQualityStringMatchesLabel(t *testing.T) {
	for _, q := range AllQualities() {
		if got, want := q.String(), q.Label(); got != want {
			t.Errorf("Quality(%d).String() = %q, want %q", int(q), got, want)
		}
	}
	if got, want := Quality(99).String(), "Unknown"; got != want {
		t.Errorf("Quality(99).String() = %q, want %q", got, want)
	}
}

func TestQualityHeight(t *testing.T) {
	tests := []struct {
		name string
		q    Quality
		want int
	}{
		{"QBest", QBest, 0},
		{"Q2160", Q2160, 2160},
		{"Q1440", Q1440, 1440},
		{"Q1080", Q1080, 1080},
		{"Q720", Q720, 720},
		{"Q480", Q480, 480},
		{"Q360", Q360, 360},
		{"QAudio", QAudio, 0},
		{"invalid 99", Quality(99), 0},
		{"invalid -1", Quality(-1), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.q.Height(); got != tt.want {
				t.Errorf("Quality(%d).Height() = %d, want %d", int(tt.q), got, tt.want)
			}
		})
	}
}

// TestQualityFormatSelector pins the exact yt-dlp -f expressions; any change
// alters download behavior, so these strings are a hard contract.
func TestQualityFormatSelector(t *testing.T) {
	tests := []struct {
		name string
		q    Quality
		want string
	}{
		{"QBest", QBest, "bv*+ba/b"},
		{"Q2160", Q2160, "bv*[height<=?2160]+ba/b[height<=?2160]/b"},
		{"Q1440", Q1440, "bv*[height<=?1440]+ba/b[height<=?1440]/b"},
		{"Q1080", Q1080, "bv*[height<=?1080]+ba/b[height<=?1080]/b"},
		{"Q720", Q720, "bv*[height<=?720]+ba/b[height<=?720]/b"},
		{"Q480", Q480, "bv*[height<=?480]+ba/b[height<=?480]/b"},
		{"Q360", Q360, "bv*[height<=?360]+ba/b[height<=?360]/b"},
		{"QAudio", QAudio, "ba/b"},
		{"invalid 99 falls back to best", Quality(99), "bv*+ba/b"},
		{"invalid -1 falls back to best", Quality(-1), "bv*+ba/b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.q.FormatSelector(); got != tt.want {
				t.Errorf("Quality(%d).FormatSelector() = %q, want %q", int(tt.q), got, tt.want)
			}
		})
	}
}

func TestQualityIsAudio(t *testing.T) {
	for _, q := range AllQualities() {
		want := q == QAudio
		if got := q.IsAudio(); got != want {
			t.Errorf("Quality(%d).IsAudio() = %v, want %v", int(q), got, want)
		}
	}
	if Quality(99).IsAudio() {
		t.Error("Quality(99).IsAudio() = true, want false for invalid quality")
	}
}
