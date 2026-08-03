package probe

import (
	"fmt"
	"strings"
)

// Method is how a file should be delivered to a client.
type Method string

const (
	// DirectPlay sends the file untouched. Cheapest by a wide margin.
	DirectPlay Method = "direct"
	// Remux repackages into a compatible container without re-encoding. Cheap:
	// no decode, no encode, just a different wrapper around the same frames.
	Remux Method = "remux"
	// Transcode re-encodes. Expensive, and the last resort.
	Transcode Method = "transcode"
)

// Decision is the outcome, with the reasoning attached.
//
// Reason is not decoration. "Why is my server pinned at 100% CPU" is the most
// common question a media server has to answer, and a decision that cannot
// explain itself makes that unanswerable.
type Decision struct {
	Method       Method `json:"method"`
	Reason       string `json:"reason"`
	VideoAction  string `json:"video_action"` // copy | encode
	AudioAction  string `json:"audio_action"` // copy | encode
	TargetFormat string `json:"target_format,omitempty"`

	// AudioOnly marks content with no picture — a music track, or a video file
	// stripped to its audio. It travels on the decision because the ffmpeg
	// command line must not map or encode a video stream that does not exist:
	// `-map 0:v:0` against a music file is a hard failure, not a degraded
	// stream, and the caller has no other way to know.
	AudioOnly bool `json:"audio_only,omitempty"`
}

// Profile describes what a client can play.
//
// Deliberately data rather than a hardcoded browser list: the same structure
// describes Chrome, a TV app, and a client that does not exist yet, and a
// wrong entry is a config change rather than a code change.
type Profile struct {
	Name string `json:"name"`

	Containers  []string `json:"containers"`
	VideoCodecs []string `json:"video_codecs"`
	AudioCodecs []string `json:"audio_codecs"`

	// MaxHeight of 0 means no limit.
	MaxHeight int `json:"max_height,omitempty"`
	// MaxVideoBitRate in bits per second; 0 means no limit.
	MaxVideoBitRate int64 `json:"max_video_bitrate,omitempty"`
	// MaxAudioChannels of 0 means no limit. Set it to force a downmix, not to
	// express what the client can decode.
	MaxAudioChannels int `json:"max_audio_channels,omitempty"`
}

// BrowserProfile is what a modern desktop browser reliably plays.
//
// Conservative on codecs, because claiming support that fails produces a black
// rectangle with no explanation — far worse than an unnecessary remux. This is
// the default for an unidentified client, so it claims only what every current
// browser can do: HEVC is excluded because Chrome's support is conditional on
// hardware and Firefox has none. Clients that can do better ask for a profile
// by name.
//
// Deliberately *not* conservative on channel count. Browsers decode
// multichannel AAC fine; the limit exists to force a downmix, not to describe
// capability. Measured against a real 225-film library, defaulting it to
// stereo needlessly transcoded 18 files whose audio the browser could already
// play. AC-3, E-AC-3, DTS, and TrueHD are genuinely unsupported and are
// excluded by codec, which is the honest reason.
//
// The bare audio containers are listed because a music library is mostly files
// whose container *is* the codec — an .mp3 reports container "mp3", a .flac
// reports "flac". Without them every track in the library failed the container
// check and was rewrapped into MP4, and FLAC does not survive that: it cannot
// be copied into fragmented MP4 (see mp4CarriesAudio), so a lossless file was
// re-encoded to AAC to play a container the browser already handles natively.
// PCM is listed at 16-bit and 8-bit only; 24-bit WAV support is not universal
// and a needless encode there is cheap, where a wrong claim is silence.
func BrowserProfile() Profile {
	return Profile{
		Name:        "browser",
		Containers:  []string{"mp4", "webm", "mov", "mp3", "flac", "ogg", "wav"},
		VideoCodecs: []string{"h264", "vp8", "vp9", "av1"},
		AudioCodecs: []string{"aac", "mp3", "opus", "vorbis", "flac", "pcm_s16le", "pcm_u8"},
	}
}

// SafariProfile is Safari on macOS, iOS and tvOS.
//
// The two things it does that the conservative default cannot: HEVC decodes
// natively (hardware, on every supported device rather than conditionally),
// and AC-3/E-AC-3 play in MP4. Both are large slices of a real library —
// excluding them costs a full video re-encode and an audio re-encode
// respectively, on files this client could have direct-played.
//
// On the music side it gains ALAC, which is Apple's own lossless codec and the
// format an iTunes-ripped library is in, and loses Ogg, which Apple has never
// shipped a decoder for. Opus stays in the codec list because it plays inside
// MP4 and CAF; an .opus file in an Ogg container still needs converting.
func SafariProfile() Profile {
	return Profile{
		Name:        "safari",
		Containers:  []string{"mp4", "mov", "mp3", "flac", "wav"},
		VideoCodecs: []string{"h264", "hevc", "av1"},
		AudioCodecs: []string{"aac", "mp3", "ac3", "eac3", "flac", "opus", "alac", "pcm_s16le", "pcm_s24le", "pcm_u8"},
	}
}

// TVProfile is a set-top or smart-TV client with a hardware decoder and a
// real container parser.
//
// Permissive on purpose: this class of device is the one that can direct-play
// the files a browser cannot, and the whole point of probing is to not burn
// CPU on a client that never needed help. DTS and TrueHD are included because
// devices in this class pass them through to a receiver.
func TVProfile() Profile {
	return Profile{
		Name: "tv",
		Containers: []string{"mp4", "matroska", "webm", "mov", "mpegts",
			"mp3", "flac", "ogg", "wav", "aac"},
		VideoCodecs: []string{"h264", "hevc", "vp9", "av1", "mpeg2video"},
		AudioCodecs: []string{"aac", "mp3", "opus", "vorbis", "flac", "alac",
			"ac3", "eac3", "dts", "truehd",
			"pcm_s16le", "pcm_s24le", "pcm_u8"},
	}
}

// ProfileByName resolves a client profile. An unknown or empty name falls back
// to the conservative browser profile: guessing generously on behalf of a
// client we cannot identify is how black rectangles happen.
func ProfileByName(name string) Profile {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "safari":
		return SafariProfile()
	case "tv":
		return TVProfile()
	default:
		return BrowserProfile()
	}
}

// Decide works out how to deliver a file to a client, using the file's default
// audio track.
func Decide(r *Result, p Profile) Decision {
	return DecideTrack(r, p, -1)
}

// DecideTrack is Decide for a caller that has chosen a specific audio track.
//
// audioIndex is an absolute stream index, or -1 for the file's default track.
// This exists because the decision and the ffmpeg `-map` must be about the
// *same* stream: deciding against the default AAC track and then mapping a
// DTS track produces `-c:a copy` on undecodable audio, and the failure shows
// up as silent playback rather than an error.
func DecideTrack(r *Result, p Profile, audioIndex int) Decision {
	if r == nil {
		// Nothing probed yet. Direct play is the honest answer: it is what
		// LANcast did before probing existed, and guessing at a transcode for
		// a file we have not inspected would burn CPU on a hunch.
		return Decision{
			Method: DirectPlay, Reason: "file has not been probed yet",
			VideoAction: "copy", AudioAction: "copy",
		}
	}

	video := r.Video()
	audio := r.Audio()
	if audioIndex >= 0 {
		if s := r.AudioByIndex(audioIndex); s != nil {
			audio = s
		}
	}

	audioOnly := video == nil && audio != nil

	videoOK, videoWhy := videoCompatible(video, p)
	audioOK, audioWhy := audioCompatible(audio, p)

	if videoOK && audioOK && contains(p.Containers, r.Container) {
		return Decision{
			Method: DirectPlay, Reason: "container and codecs are supported",
			VideoAction: "copy", AudioAction: "copy", AudioOnly: audioOnly,
		}
	}

	// Every path from here rewraps into MP4, and "the client can decode this
	// codec" is not the same claim as "MP4 can carry it". Copying a stream
	// into a container that cannot hold it is not a degraded stream — ffmpeg
	// refuses to start, and the user gets a dead player with no reason. VP8
	// and Vorbis are the common cases; FLAC in fragmented MP4 is the subtle
	// one, because it is spec'd and still widely unplayable.
	videoCopyable, videoMuxWhy := videoOK, videoWhy
	if videoOK && !mp4CarriesVideo(video) {
		videoCopyable = false
		videoMuxWhy = fmt.Sprintf("video codec %s cannot be carried in MP4", video.Codec)
	}
	audioCopyable, audioMuxWhy := audioOK, audioWhy
	if audioOK && !mp4CarriesAudio(audio) {
		audioCopyable = false
		audioMuxWhy = fmt.Sprintf("audio codec %s cannot be carried in MP4", audio.Codec)
	}

	switch {
	case videoCopyable && audioCopyable:
		// The expensive part — the pixels — is already fine. Only the wrapper
		// is wrong, and rewrapping is close to free.
		return Decision{
			Method:      Remux,
			Reason:      fmt.Sprintf("%s container is not supported, but both codecs are", r.Container),
			VideoAction: "copy", AudioAction: "copy", TargetFormat: "mp4",
			AudioOnly: audioOnly,
		}

	case videoCopyable:
		// Re-encoding audio alone is a fraction of the cost of video.
		return Decision{
			Method: Transcode, Reason: audioMuxWhy,
			VideoAction: "copy", AudioAction: "encode", TargetFormat: "mp4",
			AudioOnly: audioOnly,
		}

	default:
		// The video needs re-encoding. That is no reason to touch the audio as
		// well: copying a compatible track alongside a video encode costs
		// nothing and avoids a second generation of lossy audio.
		reason := videoMuxWhy
		audioAction := "copy"
		if !audioCopyable {
			audioAction = "encode"
			if audioMuxWhy != "" {
				reason += "; " + audioMuxWhy
			}
		}
		return Decision{
			Method: Transcode, Reason: reason,
			VideoAction: "encode", AudioAction: audioAction, TargetFormat: "mp4",
		}
	}
}

// mp4CarriesVideo reports whether a video codec can be muxed into fragmented
// MP4. Listing what works rather than what does not: an unrecognised codec
// re-encodes, which is slow but plays.
func mp4CarriesVideo(s *Stream) bool {
	if s == nil {
		return true
	}
	switch strings.ToLower(s.Codec) {
	case "h264", "hevc", "av1", "vp9", "mpeg4", "mpeg2video":
		return true
	}
	return false
}

// mp4CarriesAudio reports whether an audio codec can be muxed into fragmented
// MP4 and actually played back. FLAC and Opus are deliberately excluded: both
// are legal in MP4 by spec and both fail in enough browsers that copying them
// is a coin toss, where re-encoding to AAC costs almost nothing.
func mp4CarriesAudio(s *Stream) bool {
	if s == nil {
		return true
	}
	switch strings.ToLower(s.Codec) {
	case "aac", "mp3", "ac3", "eac3":
		return true
	}
	return false
}

func videoCompatible(s *Stream, p Profile) (bool, string) {
	if s == nil {
		// Audio-only content has no video to be incompatible.
		return true, ""
	}
	if !contains(p.VideoCodecs, s.Codec) {
		return false, fmt.Sprintf("video codec %s is not supported", s.Codec)
	}
	if p.MaxHeight > 0 && s.Height > p.MaxHeight {
		return false, fmt.Sprintf("video is %dp, above the %dp limit", s.Height, p.MaxHeight)
	}
	if p.MaxVideoBitRate > 0 && s.BitRate > p.MaxVideoBitRate {
		return false, fmt.Sprintf("video bitrate %d exceeds the limit", s.BitRate)
	}
	// 10-bit H.264 is a real trap: the codec name matches, browsers advertise
	// H.264 support, and playback still fails. High 10 is not in any browser's
	// baseline. HEVC, VP9 and AV1 carry 10-bit fine, so the rule is H.264 only.
	if strings.EqualFold(s.Codec, "h264") && isTenBit(s) {
		return false, "10-bit H.264 is not supported by browsers"
	}
	return true, ""
}

// isTenBit reports whether a stream carries more than 8 bits per component.
//
// pix_fmt is the reliable signal (yuv420p10le and friends); the profile name
// is the fallback for a probe that did not report one. Matched exactly rather
// than by substring — "10" appears in profile strings that have nothing to do
// with bit depth, and a false positive here is a needless full re-encode.
func isTenBit(s *Stream) bool {
	pix := strings.ToLower(s.PixFmt)
	if strings.Contains(pix, "p10") || strings.Contains(pix, "p12") ||
		strings.Contains(pix, "p016") || strings.Contains(pix, "p010") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(s.Profile)) {
	case "high 10", "high 10 intra", "high 4:2:2", "high 4:2:2 intra",
		"high 4:4:4 predictive", "high 4:4:4 intra":
		return true
	}
	return false
}

func audioCompatible(s *Stream, p Profile) (bool, string) {
	if s == nil {
		return true, ""
	}
	if !contains(p.AudioCodecs, s.Codec) {
		return false, fmt.Sprintf("audio codec %s is not supported", s.Codec)
	}
	if p.MaxAudioChannels > 0 && s.Channels > p.MaxAudioChannels {
		return false, fmt.Sprintf("audio has %d channels, above the %d supported",
			s.Channels, p.MaxAudioChannels)
	}
	return true, ""
}

func contains(list []string, want string) bool {
	if want == "" {
		return false
	}
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
