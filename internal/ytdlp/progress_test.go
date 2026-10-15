package ytdlp

import (
	"reflect"
	"testing"
)

// TestParseProgressLine covers every accepted template, the unknown-value
// sentinel (-1), the upper percent clamp, and malformed input. Expected
// events are compared in full, including the raw Line field.
func TestParseProgressLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want ProgressEvent
		ok   bool
	}{
		{
			name: "DL downloading with all numeric fields",
			line: "DL|vid1|downloading|1048576|10485760|NA|524288|12|10.0",
			want: ProgressEvent{
				VideoID:    "vid1",
				Stage:      StageDownloading,
				Percent:    10.0,
				SpeedBps:   524288,
				ETASeconds: 12,
				BytesDone:  1048576,
				BytesTotal: 10485760,
				Line:       "DL|vid1|downloading|1048576|10485760|NA|524288|12|10.0",
			},
			ok: true,
		},
		{
			name: "DL total NA falls back to estimate",
			line: "DL|v|downloading|5|NA|10|NA|NA|50",
			want: ProgressEvent{
				VideoID:    "v",
				Stage:      StageDownloading,
				Percent:    50,
				SpeedBps:   -1,
				ETASeconds: -1,
				BytesDone:  5,
				BytesTotal: 10,
				Line:       "DL|v|downloading|5|NA|10|NA|NA|50",
			},
			ok: true,
		},
		{
			name: "DL finished maps to finished stage at 100 percent",
			line: "DL|v|finished|10|10|NA|0|0|100",
			want: ProgressEvent{
				VideoID:    "v",
				Stage:      StageFinished,
				Percent:    100,
				SpeedBps:   0,
				ETASeconds: 0,
				BytesDone:  10,
				BytesTotal: 10,
				Line:       "DL|v|finished|10|10|NA|0|0|100",
			},
			ok: true,
		},
		{
			name: "DL percent above 100 is clamped",
			line: "DL|v|downloading|5|10|NA|NA|NA|100.5",
			want: ProgressEvent{
				VideoID:    "v",
				Stage:      StageDownloading,
				Percent:    100,
				SpeedBps:   -1,
				ETASeconds: -1,
				BytesDone:  5,
				BytesTotal: 10,
				Line:       "DL|v|downloading|5|10|NA|NA|NA|100.5",
			},
			ok: true,
		},
		{
			name: "DL percent NA is unknown",
			line: "DL|v|downloading|7|8|NA|NA|NA|NA",
			want: ProgressEvent{
				VideoID:    "v",
				Stage:      StageDownloading,
				Percent:    -1,
				SpeedBps:   -1,
				ETASeconds: -1,
				BytesDone:  7,
				BytesTotal: 8,
				Line:       "DL|v|downloading|7|8|NA|NA|NA|NA",
			},
			ok: true,
		},
		{
			name: "DL unknown status maps to downloading",
			line: "DL|v|weird|1|2|NA|NA|NA|7",
			want: ProgressEvent{
				VideoID:    "v",
				Stage:      StageDownloading,
				Percent:    7,
				SpeedBps:   -1,
				ETASeconds: -1,
				BytesDone:  1,
				BytesTotal: 2,
				Line:       "DL|v|weird|1|2|NA|NA|NA|7",
			},
			ok: true,
		},
		{
			name: "DL malformed numeric fields become unknown",
			line: "DL|v|downloading|abc|def|ghi|jkl|mno|pqr",
			want: ProgressEvent{
				VideoID:    "v",
				Stage:      StageDownloading,
				Percent:    -1,
				SpeedBps:   -1,
				ETASeconds: -1,
				BytesDone:  -1,
				BytesTotal: -1,
				Line:       "DL|v|downloading|abc|def|ghi|jkl|mno|pqr",
			},
			ok: true,
		},
		{
			name: "PP processing maps to merging stage",
			line: "PP|v|processing",
			want: ProgressEvent{
				VideoID:    "v",
				Stage:      StageMerging,
				Percent:    -1,
				SpeedBps:   -1,
				ETASeconds: -1,
				BytesDone:  -1,
				BytesTotal: -1,
				Line:       "PP|v|processing",
			},
			ok: true,
		},
		{
			name: "PP finished maps to finished stage",
			line: "PP|v|finished",
			want: ProgressEvent{
				VideoID:    "v",
				Stage:      StageFinished,
				Percent:    -1,
				SpeedBps:   -1,
				ETASeconds: -1,
				BytesDone:  -1,
				BytesTotal: -1,
				Line:       "PP|v|finished",
			},
			ok: true,
		},
		{
			name: "empty line is rejected",
			line: "",
			ok:   false,
		},
		{
			name: "DL with too few fields is rejected",
			line: "DL|a|b",
			ok:   false,
		},
		{
			name: "DL with eight fields is rejected",
			line: "DL|a|b|c|d|e|f|g",
			ok:   false,
		},
		{
			name: "DL with ten fields is rejected",
			line: "DL|a|b|c|d|e|f|g|h|i",
			ok:   false,
		},
		{
			name: "unknown prefix is rejected",
			line: "XX|a|b",
			ok:   false,
		},
		{
			name: "garbage is rejected",
			line: "garbage",
			ok:   false,
		},
		{
			name: "PP with too few fields is rejected",
			line: "PP|a",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseProgressLine(tt.line)
			if ok != tt.ok {
				t.Fatalf("ParseProgressLine(%q) ok = %v, want %v (event %+v)", tt.line, ok, tt.ok, got)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseProgressLine(%q)\n got = %+v\nwant = %+v", tt.line, got, tt.want)
			}
		})
	}
}

// TestParseProgressLineTrimsWhitespace verifies surrounding whitespace is
// stripped and the stored raw line is the trimmed form.
func TestParseProgressLineTrimsWhitespace(t *testing.T) {
	const raw = " \tDL|v|finished|10|10|NA|0|0|100\r\n"

	got, ok := ParseProgressLine(raw)
	if !ok {
		t.Fatalf("ParseProgressLine(%q) ok = false, want true", raw)
	}
	const wantLine = "DL|v|finished|10|10|NA|0|0|100"
	if got.Line != wantLine {
		t.Errorf("ParseProgressLine(%q).Line = %q, want %q", raw, got.Line, wantLine)
	}
	if got.Stage != StageFinished || got.Percent != 100 {
		t.Errorf("ParseProgressLine(%q) = %+v, want finished stage at 100 percent", raw, got)
	}
}

func TestStageString(t *testing.T) {
	tests := []struct {
		stage Stage
		want  string
	}{
		{StageInitializing, "initializing"},
		{StageDownloading, "downloading"},
		{StageMerging, "merging"},
		{StagePostprocessing, "postprocessing"},
		{StageFinished, "finished"},
		{StageError, "error"},
		{Stage(99), "unknown"},
		{Stage(-1), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.stage.String(); got != tt.want {
			t.Errorf("Stage(%d).String() = %q, want %q", int(tt.stage), got, tt.want)
		}
	}
}
