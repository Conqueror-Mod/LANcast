package probe

import (
	"context"
	"strings"
	"testing"
)

// A typical MKV: H.264 video, multichannel DTS, embedded subtitles.
const mkvJSON = `{
 "streams":[
  {"index":0,"codec_type":"video","codec_name":"h264","profile":"High","width":1920,"height":1080,
   "pix_fmt":"yuv420p","avg_frame_rate":"24000/1001","bit_rate":"8000000",
   "disposition":{"default":1},"tags":{}},
  {"index":1,"codec_type":"audio","codec_name":"dts","profile":"DTS-HD MA","channels":6,
   "sample_rate":"48000","disposition":{"default":1},"tags":{"language":"eng","title":"Surround"}},
  {"index":2,"codec_type":"subtitle","codec_name":"subrip",
   "disposition":{"default":0,"forced":0},"tags":{"language":"eng"}},
  {"index":3,"codec_type":"subtitle","codec_name":"subrip",
   "disposition":{"forced":1},"tags":{"language":"fre"}},
  {"index":4,"codec_type":"attachment","codec_name":"ttf","tags":{}}
 ],
 "format":{"format_name":"matroska,webm","duration":"7284.512000","size":"8123456789","bit_rate":"8900000"}
}`

func TestParseMKV(t *testing.T) {
	r, err := ParseJSON([]byte(mkvJSON))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}

	if r.Container != "matroska" {
		t.Errorf("Container = %q, want the first name only", r.Container)
	}
	if r.DurationMS != 7284512 {
		t.Errorf("DurationMS = %d, want 7284512", r.DurationMS)
	}
	if r.SizeBytes != 8123456789 {
		t.Errorf("SizeBytes = %d", r.SizeBytes)
	}

	// The attachment must not become a track.
	if len(r.Streams) != 4 {
		t.Fatalf("streams = %d, want 4 (the ttf attachment excluded)", len(r.Streams))
	}

	v := r.Video()
	if v == nil {
		t.Fatal("no video stream")
	}
	if v.Codec != "h264" || v.Width != 1920 || v.Height != 1080 {
		t.Errorf("video = %+v", v)
	}
	if v.FrameRate != "23.976023976023978" {
		t.Errorf("FrameRate = %q, want the rational converted", v.FrameRate)
	}

	a := r.Audio()
	if a == nil || a.Codec != "dts" || a.Channels != 6 {
		t.Errorf("audio = %+v", a)
	}
	if a.Language != "eng" || a.Title != "Surround" {
		t.Errorf("audio tags = %q / %q", a.Language, a.Title)
	}

	subs := r.Subtitles()
	if len(subs) != 2 {
		t.Fatalf("subtitles = %d, want 2", len(subs))
	}
	if !subs[1].Forced {
		t.Error("the forced flag was not read")
	}
}

// Cover art is stored as a video stream. Treating it as the picture would make
// an audio file look like a film.
func TestCoverArtIsNotTreatedAsVideo(t *testing.T) {
	const json = `{"streams":[
	 {"index":0,"codec_type":"audio","codec_name":"flac","channels":2,"disposition":{"default":1}},
	 {"index":1,"codec_type":"video","codec_name":"mjpeg","width":600,"height":600,"disposition":{}}
	],"format":{"format_name":"flac","duration":"240.0"}}`

	r, err := ParseJSON([]byte(json))
	if err != nil {
		t.Fatal(err)
	}
	if v := r.Video(); v != nil {
		t.Errorf("Video() = %+v, want nil — mjpeg here is cover art", v)
	}
	if a := r.Audio(); a == nil || a.Codec != "flac" {
		t.Errorf("Audio() = %+v", a)
	}
}

// "und" carries no more information than an empty string but reads as a real
// language in a track picker.
func TestUndefinedLanguageIsBlanked(t *testing.T) {
	const json = `{"streams":[{"index":0,"codec_type":"audio","codec_name":"aac",
	 "disposition":{"default":1},"tags":{"language":"und"}}],"format":{"format_name":"mp4"}}`

	r, _ := ParseJSON([]byte(json))
	if got := r.Audio().Language; got != "" {
		t.Errorf("Language = %q, want empty for und", got)
	}
}

// MPEG-TS often carries no format duration; the stream has it instead.
func TestDurationFallsBackToStream(t *testing.T) {
	const json = `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264",
	 "width":1280,"height":720,"duration":"1800.5","disposition":{"default":1}}],
	 "format":{"format_name":"mpegts"}}`

	r, _ := ParseJSON([]byte(json))
	if r.DurationMS != 1800500 {
		t.Errorf("DurationMS = %d, want the stream duration used", r.DurationMS)
	}
}

func TestDefaultAudioPreferred(t *testing.T) {
	const json = `{"streams":[
	 {"index":0,"codec_type":"audio","codec_name":"ac3","channels":6,"disposition":{"default":0},"tags":{"language":"eng"}},
	 {"index":1,"codec_type":"audio","codec_name":"aac","channels":2,"disposition":{"default":1},"tags":{"language":"jpn"}}
	],"format":{"format_name":"mp4"}}`

	r, _ := ParseJSON([]byte(json))
	if a := r.Audio(); a == nil || a.Codec != "aac" {
		t.Errorf("Audio() = %+v, want the stream marked default", a)
	}
}

func TestFirstAudioUsedWhenNoDefault(t *testing.T) {
	const json = `{"streams":[
	 {"index":0,"codec_type":"audio","codec_name":"ac3","channels":6,"disposition":{}},
	 {"index":1,"codec_type":"audio","codec_name":"aac","channels":2,"disposition":{}}
	],"format":{"format_name":"mp4"}}`

	r, _ := ParseJSON([]byte(json))
	if a := r.Audio(); a == nil || a.Codec != "ac3" {
		t.Errorf("Audio() = %+v, want the first track", a)
	}
}

func TestParseGarbage(t *testing.T) {
	if _, err := ParseJSON([]byte("not json")); err == nil {
		t.Error("ParseJSON accepted garbage")
	}
}

func TestParseEmptyDocument(t *testing.T) {
	r, err := ParseJSON([]byte(`{}`))
	if err != nil {
		t.Fatalf("an empty document should parse: %v", err)
	}
	if r.Video() != nil || r.Audio() != nil || len(r.Subtitles()) != 0 {
		t.Error("an empty document produced streams")
	}
}

func TestNormalizeRate(t *testing.T) {
	tests := []struct{ in, want string }{
		{"24000/1001", "23.976023976023978"},
		{"25/1", "25"},
		{"0/0", ""},
		{"", ""},
		{"30", "30"},
	}
	for _, tt := range tests {
		if got := normalizeRate(tt.in); got != tt.want {
			t.Errorf("normalizeRate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The binary may legitimately be absent; that must be a clean error rather
// than a panic or a hang.
func TestProbeWithoutFFprobe(t *testing.T) {
	p := &Prober{Path: ""}
	p.Path = "definitely-not-a-real-binary-name"

	_, err := p.Probe(context.Background(), "whatever.mkv")
	if err == nil {
		t.Fatal("want an error when ffprobe is missing")
	}
	if strings.Contains(err.Error(), "panic") {
		t.Errorf("error = %v", err)
	}
}

func TestAvailable(t *testing.T) {
	if (&Prober{Path: "definitely-not-real"}).Available() != true {
		// An explicit path is taken at face value; existence is checked when
		// it runs. Only PATH lookup can report unavailability up front.
		t.Log("explicit paths are not verified up front, as documented")
	}
	if (&Prober{}).Available() {
		t.Log("ffprobe is installed in this environment")
	}
}
