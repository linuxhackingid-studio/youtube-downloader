package ytdlp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

// strPoint returns a pointer to s, for building the pointer-based JSON views
// that playlistJSON uses to tell a missing key from an empty one.
func strPoint(s string) *string { return &s }

// intPoint returns a pointer to i.
func intPoint(i int) *int { return &i }

// floatPoint returns a pointer to f.
func floatPoint(f float64) *float64 { return &f }

// TestValidatePlaylistURL pins URL acceptance and rejection. Validation is
// scheme-only on top of url.Parse: HTTP(S) is accepted, everything else -
// including empty input and strings url.Parse cannot handle - is rejected
// with an "invalid playlist URL" diagnostic.
func TestValidatePlaylistURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantSub string // required error substring; empty means the URL must be accepted
	}{
		{"https URL", "https://www.youtube.com/playlist?list=PL1", ""},
		{"http URL", "http://example.com/watch?v=abc", ""},
		{"mixed case scheme is lowercased by url.Parse", "hTtPs://example.com/x", ""},
		{"upper case scheme is lowercased by url.Parse", "HTTPS://example.com/x", ""},
		{"opaque https form passes the scheme check", "https:example.com", ""},
		{"empty string", "", "invalid playlist URL: empty"},
		{"whitespace only has an empty scheme", "   ", `unsupported scheme ""`},
		{"plain word has an empty scheme", "youtube", `unsupported scheme ""`},
		{"scheme-relative URL has an empty scheme", "//example.com/x", `unsupported scheme ""`},
		{"ftp scheme", "ftp://example.com/x", `unsupported scheme "ftp"`},
		{"javascript scheme", "javascript:alert(1)", `unsupported scheme "javascript"`},
		{"leading whitespace is unparsable", " https://example.com", "invalid playlist URL: parse"},
		{"host whitespace is unparsable", "https://exa mple.com", "invalid playlist URL: parse"},
		{"broken percent escape is unparsable", "https://example.com/%zz", "invalid playlist URL: parse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePlaylistURL(tt.raw)
			if tt.wantSub == "" {
				if err != nil {
					t.Fatalf("validatePlaylistURL(%q) = %v, want nil", tt.raw, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validatePlaylistURL(%q) = nil, want an error containing %q", tt.raw, tt.wantSub)
			}
			if got := err.Error(); !strings.Contains(got, tt.wantSub) {
				t.Errorf("validatePlaylistURL(%q) = %q, want it to contain %q", tt.raw, got, tt.wantSub)
			}
		})
	}
}

// TestDeref pins the nil-tolerant string dereference.
func TestDeref(t *testing.T) {
	if got := deref(nil); got != "" {
		t.Errorf("deref(nil) = %q, want the empty string", got)
	}
	if got := deref(strPoint("value")); got != "value" {
		t.Errorf("deref(strPoint(%q)) = %q, want %q", "value", got, "value")
	}
	if got := deref(strPoint("")); got != "" {
		t.Errorf("deref(strPoint(\"\")) = %q, want the empty string", got)
	}
}

// TestToPlaylistMapsUsableEntries pins the entry mapping: unusable entries
// are skipped without shifting the positions of later ones, playlist_index
// wins over the array position only when positive, and title, URL, and
// duration fall back to their documented values.
func TestToPlaylistMapsUsableEntries(t *testing.T) {
	root := &playlistJSON{
		ID:    strPoint("PL1"),
		Title: strPoint("My Playlist"),
		Entries: []entryJSON{
			{
				ID:            strPoint("v1"),
				Title:         strPoint("First"),
				URL:           strPoint("https://www.youtube.com/watch?v=v1"),
				Duration:      floatPoint(12.5),
				PlaylistIndex: intPoint(7),
			},
			{Title: strPoint("missing id is skipped")},
			{ID: strPoint("")},
			{ID: strPoint("v2")},
			{
				ID:            strPoint("v3"),
				Duration:      floatPoint(-4),
				PlaylistIndex: intPoint(0),
			},
			{
				ID:            strPoint("v4"),
				URL:           strPoint("https://example.com/other"),
				Duration:      floatPoint(0),
				PlaylistIndex: intPoint(-2),
			},
		},
	}

	got := root.toPlaylist()
	want := &Playlist{
		ID:    "PL1",
		Title: "My Playlist",
		Videos: []Video{
			{Index: 7, ID: "v1", Title: "First", URL: "https://www.youtube.com/watch?v=v1", Duration: 12.5},
			{Index: 4, ID: "v2", Title: "Video v2", URL: watchURLPrefix + "v2"},
			{Index: 5, ID: "v3", Title: "Video v3", URL: watchURLPrefix + "v3"},
			{Index: 6, ID: "v4", Title: "Video v4", URL: "https://example.com/other"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("toPlaylist() mismatch\n got = %+v\nwant = %+v", got, want)
	}
}

// TestToPlaylistPromotesSingleVideo pins the single-video promotion rule: a
// zero-entry root becomes a one-entry playlist only when it has an id and
// looks like a video - type "video" or a url containing "watch". Anything
// else (a playlist whose entries were all unusable, a root without an id)
// stays empty.
func TestToPlaylistPromotesSingleVideo(t *testing.T) {
	tests := []struct {
		name string
		raw  playlistJSON
		want []Video
	}{
		{
			name: "video type with title",
			raw:  playlistJSON{ID: strPoint("id1"), Title: strPoint("Solo"), Type: strPoint("video")},
			want: []Video{{Index: 1, ID: "id1", Title: "Solo", URL: watchURLPrefix + "id1"}},
		},
		{
			name: "video type without title",
			raw:  playlistJSON{ID: strPoint("id2"), Type: strPoint("video")},
			want: []Video{{Index: 1, ID: "id2", Title: "Video id2", URL: watchURLPrefix + "id2"}},
		},
		{
			name: "watch url without type",
			raw:  playlistJSON{ID: strPoint("id3"), URL: strPoint("https://www.youtube.com/watch?v=id3")},
			want: []Video{{Index: 1, ID: "id3", Title: "Video id3", URL: watchURLPrefix + "id3"}},
		},
		{
			name: "unusable entries still promote a video root",
			raw: playlistJSON{
				ID:      strPoint("id4"),
				Type:    strPoint("video"),
				Entries: []entryJSON{{Title: strPoint("no id")}},
			},
			want: []Video{{Index: 1, ID: "id4", Title: "Video id4", URL: watchURLPrefix + "id4"}},
		},
		{
			name: "usable entries suppress promotion",
			raw: playlistJSON{
				ID:      strPoint("id5"),
				Type:    strPoint("video"),
				Entries: []entryJSON{{ID: strPoint("e1")}},
			},
			want: []Video{{Index: 1, ID: "e1", Title: "Video e1", URL: watchURLPrefix + "e1"}},
		},
		{
			name: "playlist root with no usable entries stays empty",
			raw: playlistJSON{
				ID:      strPoint("PL2"),
				Type:    strPoint("playlist"),
				URL:     strPoint("https://www.youtube.com/playlist?list=PL2"),
				Entries: []entryJSON{{Title: strPoint("no id")}},
			},
			want: []Video{},
		},
		{
			name: "video root without id stays empty",
			raw:  playlistJSON{Type: strPoint("video")},
			want: []Video{},
		},
		{
			name: "zero root stays empty",
			raw:  playlistJSON{},
			want: []Video{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := tt.raw
			got := raw.toPlaylist()
			if !reflect.DeepEqual(got.Videos, tt.want) {
				t.Errorf("toPlaylist().Videos mismatch\n got = %+v\nwant = %+v", got.Videos, tt.want)
			}
		})
	}
}

// TestPlaylistEntryLimit pins the entry cap: a listing with exactly
// maxPlaylistEntries entries is accepted, and one entry more is rejected
// with a deterministic error before any entry is converted. The JSON is
// built programmatically, so the test stays fast and independent of any
// fixture size.
func TestPlaylistEntryLimit(t *testing.T) {
	decode := func(entries int) *playlistJSON {
		t.Helper()

		var b strings.Builder
		b.WriteString(`{"id":"PL1","entries":[`)
		for i := 0; i < entries; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"id":"v%d"}`, i)
		}
		b.WriteString(`]}`)

		var raw playlistJSON
		if err := json.NewDecoder(strings.NewReader(b.String())).Decode(&raw); err != nil {
			t.Fatalf("decode a %d-entry listing: %v", entries, err)
		}
		if len(raw.Entries) != entries {
			t.Fatalf("decoded %d entries, want %d", len(raw.Entries), entries)
		}
		return &raw
	}

	if err := decode(maxPlaylistEntries).checkEntryLimit(); err != nil {
		t.Errorf("checkEntryLimit() at the limit = %v, want nil", err)
	}

	err := decode(maxPlaylistEntries + 1).checkEntryLimit()
	if err == nil {
		t.Fatal("checkEntryLimit() above the limit = nil, want an error")
	}
	want := fmt.Sprintf("playlist too large (>%d entries)", maxPlaylistEntries)
	if got := err.Error(); got != want {
		t.Errorf("checkEntryLimit() error = %q, want %q", got, want)
	}
}

// TestPlaylistReaderByteCap pins the byte cap that bounds what FetchPlaylist
// decodes, so a hostile host cannot force a multi-gigabyte listing into
// memory: reads within the limit pass through untouched, a listing that ends
// exactly at the limit still decodes, and a stream that continues past it
// fails with errPlaylistTooLarge. A bare io.LimitReader would instead truncate
// the body and surface as a confusing "unexpected end of JSON input" from the
// decoder. The limit is a parameter of newPlaylistReader so these cases run
// against a few dozen bytes instead of the real 256 MiB cap.
func TestPlaylistReaderByteCap(t *testing.T) {
	const limit = 64

	t.Run("a listing under the limit decodes untouched", func(t *testing.T) {
		body := `{"id":"PL1","title":"ok"}`
		var raw playlistJSON
		if err := json.NewDecoder(newPlaylistReader(strings.NewReader(body), limit)).Decode(&raw); err != nil {
			t.Fatalf("decode a %d-byte listing under a %d-byte cap: %v", len(body), limit, err)
		}
		if got := deref(raw.ID); got != "PL1" {
			t.Errorf("decoded id = %q, want %q", got, "PL1")
		}
	})

	t.Run("a listing ending exactly at the limit decodes", func(t *testing.T) {
		body := `{"id":"` + strings.Repeat("v", limit-len(`{"id":""}`)) + `"}`
		if len(body) != limit {
			t.Fatalf("test bug: listing is %d bytes, want exactly the %d-byte limit", len(body), limit)
		}
		var raw playlistJSON
		if err := json.NewDecoder(newPlaylistReader(strings.NewReader(body), limit)).Decode(&raw); err != nil {
			t.Fatalf("decode a listing of exactly the %d-byte limit: %v", limit, err)
		}
	})

	t.Run("a listing one byte over the limit fails the cap", func(t *testing.T) {
		body := `{"id":"` + strings.Repeat("v", limit-len(`{"id":""}`)+1) + `"}`
		if len(body) != limit+1 {
			t.Fatalf("test bug: listing is %d bytes, want the %d-byte limit plus one", len(body), limit)
		}
		var raw playlistJSON
		err := json.NewDecoder(newPlaylistReader(strings.NewReader(body), limit)).Decode(&raw)
		if err == nil {
			t.Fatal("decode of a listing one byte over the cap = nil, want errPlaylistTooLarge")
		}
		if !errors.Is(err, errPlaylistTooLarge) {
			t.Fatalf("decode error = %v (%T), want it to wrap errPlaylistTooLarge", err, err)
		}
		const want = "playlist response exceeds size limit"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("decode error = %q, want it to contain %q", err, want)
		}
	})

	t.Run("the cap reports itself rather than a truncated body", func(t *testing.T) {
		// Exercise Read directly through io.Copy: the cap must stop the
		// stream after exactly limit bytes and say why, not mimic EOF.
		r := newPlaylistReader(strings.NewReader(strings.Repeat("x", 2*limit)), limit)
		n, err := io.Copy(io.Discard, r)
		if !errors.Is(err, errPlaylistTooLarge) {
			t.Fatalf("copying an oversized stream = %v, want errPlaylistTooLarge", err)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("copy error = %v, want a size-limit diagnostic rather than EOF", err)
		}
		if n != limit {
			t.Errorf("copied %d bytes before the cap error, want %d", n, limit)
		}
	})
}

// TestTailBufferKeepsOnlyTail pins the stderr cap: every write is accepted in
// full so the exec stderr copier never blocks, but only the last max bytes
// survive, whether they arrive as small writes that shift the retained window
// or as one write larger than the cap. The final subtest drives the real 64
// KiB cap, far more than exitErrorFrom ever reports (the last ten lines).
func TestTailBufferKeepsOnlyTail(t *testing.T) {
	t.Run("writes under the cap are retained whole", func(t *testing.T) {
		b := newTailBuffer(16)
		n, err := b.Write([]byte("hello"))
		if n != 5 || err != nil {
			t.Fatalf("Write = (%d, %v), want (5, nil)", n, err)
		}
		if got := b.String(); got != "hello" {
			t.Errorf("String() = %q, want %q", got, "hello")
		}
	})

	t.Run("small writes keep only the last max bytes", func(t *testing.T) {
		b := newTailBuffer(16)
		if _, err := b.Write([]byte("0123456789abcdef")); err != nil {
			t.Fatalf("Write of the first 16 bytes: %v", err)
		}
		if _, err := b.Write([]byte("ABCDEF")); err != nil {
			t.Fatalf("Write of the trailing 6 bytes: %v", err)
		}
		const want = "6789abcdefABCDEF"
		if got := b.String(); got != want {
			t.Errorf("String() = %q, want the stream's last 16 bytes %q", got, want)
		}
	})

	t.Run("one oversized write keeps its tail", func(t *testing.T) {
		b := newTailBuffer(8)
		if _, err := b.Write([]byte("0123456789ABC")); err != nil {
			t.Fatalf("Write of 13 bytes into an 8-byte buffer: %v", err)
		}
		const want = "56789ABC"
		if got := b.String(); got != want {
			t.Errorf("String() = %q, want the write's last 8 bytes %q", got, want)
		}
	})

	t.Run("a stream past the real cap stays bounded", func(t *testing.T) {
		b := newTailBuffer(maxPlaylistStderrBytes)
		head := strings.Repeat("h", maxPlaylistStderrBytes)
		tail := strings.Repeat("t", 4096)
		for _, chunk := range []string{head, tail} {
			n, err := b.Write([]byte(chunk))
			if n != len(chunk) || err != nil {
				t.Fatalf("Write of %d bytes = (%d, %v), want (%d, nil)", len(chunk), n, err, len(chunk))
			}
		}

		got := b.String()
		if len(got) != maxPlaylistStderrBytes {
			t.Fatalf("retained %d bytes after writing %d, want the %d-byte cap",
				len(got), len(head)+len(tail), maxPlaylistStderrBytes)
		}
		want := strings.Repeat("h", maxPlaylistStderrBytes-len(tail)) + tail
		if got != want {
			t.Errorf("String() did not retain the stream's last %d bytes", maxPlaylistStderrBytes)
		}
	})
}
