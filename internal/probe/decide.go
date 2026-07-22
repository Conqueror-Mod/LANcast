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
// rectangle with no explanation — far worse than an unnecessary remux.
//
// Deliberately *not* conservative on channel count. Browsers decode
// multichannel AAC fine; the limit exists to force a downmix, not to describe
// capability. Measured against a real 225-film library, defaulting it to
// stereo needlessly transcoded 18 files whose audio the browser could already
// play. AC-3, E-AC-3, DTS, and TrueHD are genuinely unsupported and are
// excluded by codec, which is the honest reason.
func BrowserProfile() Profile {
	return Profile{
		Name:        "browser",
		Containers:  []string{"mp4", "webm", "mov"},
		VideoCodecs: []string{"h264", "vp8", "vp9", "av1"},
		AudioCodecs: []string{"aac", "mp3", "opus", "vorbis", "flac"},
	}
}

// Decide works out how to deliver a file to a client.
func Decide(r *Result, p Profile) Decision {
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

	videoOK, videoWhy := videoCompatible(video, p)
	audioOK, audioWhy := audioCompatible(audio, p)
	containerOK := contains(p.Containers, r.Container)

	switch {
	case videoOK && audioOK && containerOK:
		return Decision{
			Method: DirectPlay, Reason: "container and codecs are supported",
			VideoAction: "copy", AudioAction: "copy",
		}

	case videoOK && audioOK:
		// The expensive part — the pixels — is already fine. Only the wrapper
		// is wrong, and rewrapping is close to free.
		return Decision{
			Method:      Remux,
			Reason:      fmt.Sprintf("%s container is not supported, but both codecs are", r.Container),
			VideoAction: "copy", AudioAction: "copy", TargetFormat: "mp4",
		}

	case videoOK && !audioOK:
		// Re-encoding audio alone is a fraction of the cost of video.
		return Decision{
			Method: Transcode, Reason: audioWhy,
			VideoAction: "copy", AudioAction: "encode", TargetFormat: "mp4",
		}

	default:
		reason := videoWhy
		if !audioOK && audioWhy != "" {
			reason += "; " + audioWhy
		}
		return Decision{
			Method: Transcode, Reason: reason,
			VideoAction: "encode", AudioAction: "encode", TargetFormat: "mp4",
		}
	}
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
	// baseline. HEVC and AV1 carry 10-bit fine.
	if s.Codec == "h264" && strings.Contains(strings.ToLower(s.Profile), "10") {
		return false, "10-bit H.264 is not supported by browsers"
	}
	return true, ""
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
