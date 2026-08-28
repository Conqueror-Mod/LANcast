package probe

import (
	"fmt"
	"strconv"
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

	// TargetHeight and TargetVideoBitRate are the ceiling the re-encode must
	// come in under, 0 meaning "no ceiling". They ride on the decision for the
	// same reason AudioOnly does: the decision is the only thing that knows a
	// limit was what forced the encode, and ffmpeg is built from it rather than
	// from the profile. Set only when VideoAction is "encode" — a copy cannot
	// honour a ceiling, and claiming otherwise would have the client believe a
	// cap applied that never reached a pixel.
	TargetHeight       int   `json:"target_height,omitempty"`
	TargetVideoBitRate int64 `json:"target_video_bitrate,omitempty"`

	/*
	 * SourceWidth and SourceHeight are the frame the encoder will be handed,
	 * before any TargetHeight scaling.
	 *
	 * They exist because H.264 has a *level*, and a level is a promise about
	 * frame size that a hardware encoder enforces. Stating one that the frame
	 * exceeds is not a hint the encoder rounds up — NVENC answers
	 * `InitializeEncoder failed: invalid param (8): Invalid Level` and produces
	 * nothing at all.
	 *
	 * They ride on the decision for the reason the fields above do: the
	 * decision is the only thing that has seen the stream, and ffmpeg is built
	 * from the decision rather than from the probe. Set only when VideoAction
	 * is "encode", since nothing else needs a level.
	 *
	 * Height alone is not enough and that is the trap this was found in. A
	 * 2160x1080 scope master is *1080 tall* — under every height cap in the
	 * system — and 2160 wide, which is what puts it over the frame-size limit.
	 * The same shape as resolution buckets reading width rather than height.
	 */
	SourceWidth  int `json:"source_width,omitempty"`
	SourceHeight int `json:"source_height,omitempty"`
	/*
	 * SourceFrameRate carries the rate for the same reason, and it matters for
	 * exactly one distinction: 4K at 30 and 4K at 60 fit the same frame-size
	 * limit and different throughput limits. Zero when the source did not say,
	 * which the level rule treats as "do not consult", never as "zero".
	 */
	SourceFrameRate float64 `json:"source_frame_rate,omitempty"`

	/*
	 * TonemapHDR marks a re-encode whose source is HDR and whose output is not
	 * (ADR 0033). It rides on the decision for the reason the fields above do:
	 * the decision is the only thing that has seen the stream's colour
	 * metadata, and ffmpeg is built from the decision rather than from the
	 * probe.
	 *
	 * Set only when VideoAction is "encode". A copy delivers the source's own
	 * video bytes, which are HDR and are correctly described as HDR — there is
	 * no conversion to perform and nothing to re-tag. Claiming otherwise would
	 * have the command line tag a passthrough stream bt709 and produce exactly
	 * the misdescribed file this flag exists to prevent.
	 */
	TonemapHDR bool `json:"tonemap_hdr,omitempty"`
}

/*
 * Encoding reports whether either stream is genuinely re-encoded.
 *
 * The distinction the name carries is a cost one: a remux rewrites the
 * container and copies the streams, which is a few percent of one core, while
 * an encode is most of one. Anything reporting to a person what the server is
 * doing has to be able to tell them apart, or a copy gets announced as a
 * transcode and the machine looks busier than it is.
 */
func (d Decision) Encoding() bool {
	return d.VideoAction == "encode" || d.AudioAction == "encode"
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

	/*
	 * Claims are the capability names the client sent, recorded as well as
	 * applied.
	 *
	 * Most claims widen a codec list and nothing else needs to know they
	 * happened. Some are permissions rather than codecs — "hevc10" authorises a
	 * bit depth of a codec that is already playable — and those can only be
	 * asked for by name.
	 */
	Claims []string `json:"claims,omitempty"`
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
// check and was rewrapped into MP4 to play a container the browser already
// handles natively. That rewrap used to cost the lossless too, since FLAC could
// not be copied into fragmented MP4; it can now (see mp4CarriesAudio), so the
// remaining reason to list the bare containers is simply that a rewrap nobody
// needs is still work nobody needs.
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

// knownCapabilities is what a client may claim, and the container each claim
// implies.
//
// A closed set on purpose. The parameter arrives from a client, and an open one
// would mean the server's idea of what it can mux is decided by whoever is
// asking; an unrecognised claim is ignored rather than trusted, so a typo or a
// future codec name degrades to today's behaviour instead of to a broken
// stream.
//
// Containers ride along because claiming a codec without the box it comes in is
// useless: an HEVC file is almost always in MP4 or Matroska, and allowing the
// codec while still failing the container check would swap a full re-encode for
// a remux rather than for direct play.
var knownCapabilities = map[string]struct {
	video      []string
	audio      []string
	containers []string
}{
	// Chromium on Windows decodes HEVC in hardware where the GPU supports it,
	// and Safari always has. Left out of the browser floor because it is
	// conditional (docs/api.md), which is exactly the sort of thing the client
	// can check and the server cannot.
	"hevc": {video: []string{"hevc"}, containers: []string{"matroska"}},
	/*
	 * 10-bit HEVC is a separate claim from HEVC, because it is a separate
	 * question and the answers differ.
	 *
	 * A browser answers `canPlayType` for Main profile 8-bit and for Main 10
	 * independently, and on Windows the first can be yes while the second is
	 * decoded badly enough to glitch. Reported exactly that way: a Main 10 film
	 * direct-played with perfect audio and a picture that stuttered, because the
	 * client had claimed "hevc" from an 8-bit probe and the server took it as
	 * covering both.
	 *
	 * It adds no codec of its own. It is permission for a bit depth, checked in
	 * canDirectPlay, and a client that does not send it gets the file
	 * transcoded — which is the behaviour that was correct all along for a
	 * decoder that cannot really manage it.
	 */
	"hevc10": {},
	// AC-3 and E-AC-3 in MP4: a large slice of a real library, and an audio
	// re-encode on every file that has it.
	"ac3":  {audio: []string{"ac3"}},
	"eac3": {audio: []string{"eac3"}},
	"dts":  {audio: []string{"dts"}},
	// A container claim on its own — a client with a real demuxer rather than a
	// browser's narrow one.
	"matroska": {containers: []string{"matroska"}},
	/*
	 * FLAC and Opus *inside MP4*, which is a different question from whether
	 * the client can decode them at all.
	 *
	 * Both are already in the browser floor, because a .flac or .opus file
	 * plays natively. Neither could be *carried* into fragmented MP4, so a file
	 * needing only a container rewrite had its audio re-encoded — and for FLAC
	 * that is lossless becoming AAC to change a box.
	 *
	 * A claim rather than a widening of the floor, because the original
	 * decision here was right and is documented: FLAC in fragmented MP4 is
	 * legal by spec and unplayable in enough browsers that copying it blind is
	 * a coin toss. Chromium manages both — verified by muxing real files and
	 * watching it reach readyState 4 — and Chromium is not every browser. So
	 * the client answers for the exact MP4 codec string and only then is the
	 * lossless kept.
	 *
	 * Permissions rather than codecs, like hevc10: they add nothing to play,
	 * they authorise a container.
	 */
	"flacmp4": {},
	"opusmp4": {},
	/*
	 * 10-bit H.264, named for the profile rather than the bit depth because
	 * "High 10" belongs to H.264 alone — HEVC's is Main 10 — so the claim
	 * cannot be misread as covering both the way `hevc` once was.
	 *
	 * A permission, not a codec: H.264 is already in every profile, and what
	 * is being authorised is a bit depth the baseline excludes.
	 */
	"high10": {},
}

/*
 * Allows reports whether a claim was made.
 *
 * Kept alongside the widened codec lists because some claims are permissions
 * rather than codecs: "hevc10" adds nothing to play, it authorises a bit depth
 * of something already playable.
 */
func (p Profile) Allows(claim string) bool {
	for _, c := range p.Claims {
		if strings.EqualFold(c, claim) {
			return true
		}
	}
	return false
}

// WithCapabilities returns p widened by what a client says it can also play.
//
// **Only ever widens.** The profile is the floor and a claim can add to it; a
// claim can never take something away, so a client that reports nonsense, or
// detects badly, is no worse off than one that says nothing. That asymmetry is
// what makes trusting the parameter safe: the worst case of a wrong claim is
// the failure the client itself will see and can retry around
// (docs/client-capabilities-plan.md), never a worse decision for anyone else.
//
// Unknown claims are dropped silently. They are not an error worth failing a
// playback over — an older server meeting a newer client should serve the file,
// not refuse it.
func WithCapabilities(p Profile, claims []string) Profile {
	out := p
	out.VideoCodecs = append([]string(nil), p.VideoCodecs...)
	out.AudioCodecs = append([]string(nil), p.AudioCodecs...)
	out.Containers = append([]string(nil), p.Containers...)

	for _, raw := range claims {
		name := strings.ToLower(strings.TrimSpace(raw))
		cap, ok := knownCapabilities[name]
		if !ok {
			continue
		}
		// Recorded as well as applied: a claim that grants a permission rather
		// than a codec has nothing to append, and canDirectPlay asks for it by
		// name.
		out.Claims = appendMissing(out.Claims, name)
		out.VideoCodecs = appendMissing(out.VideoCodecs, cap.video...)
		out.AudioCodecs = appendMissing(out.AudioCodecs, cap.audio...)
		out.Containers = appendMissing(out.Containers, cap.containers...)
	}
	return out
}

// appendMissing adds values not already present, so a claim that duplicates the
// floor changes nothing.
func appendMissing(list []string, add ...string) []string {
	for _, a := range add {
		found := false
		for _, existing := range list {
			if existing == a {
				found = true
				break
			}
		}
		if !found {
			list = append(list, a)
		}
	}
	return list
}

// ParseCapabilities splits the `can=` parameter. Empty in, nothing out.
func ParseCapabilities(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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
	// Whether a track other than the file's own has been asked for. Direct play
	// serves the file's bytes and nothing else, so it cannot honour that choice:
	// the browser opens the whole file and picks a track by its own rules. A
	// second track that happens to be playable would then be selected in the UI
	// and silently ignored in the output — the worst of the three outcomes,
	// because it looks like it worked.
	alternateAudio := false
	if audioIndex >= 0 {
		if s := r.AudioByIndex(audioIndex); s != nil {
			alternateAudio = audio == nil || s.Index != audio.Index
			audio = s
		}
	}

	audioOnly := video == nil && audio != nil

	videoOK, videoWhy := videoCompatible(video, p)
	audioOK, audioWhy := audioCompatible(audio, p)

	if videoOK && audioOK && contains(p.Containers, r.Container) && !alternateAudio {
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
	if audioOK && !mp4CarriesAudio(audio, p) {
		audioCopyable = false
		audioMuxWhy = fmt.Sprintf("audio codec %s cannot be carried in MP4", audio.Codec)
	}

	switch {
	case videoCopyable && audioCopyable:
		// The expensive part — the pixels — is already fine. Only the wrapper
		// is wrong, and rewrapping is close to free.
		//
		// The container may in fact be fine and the rewrap happen only because
		// a different audio track was asked for. Saying "the container is not
		// supported" there would be a plain lie in the one place the user is
		// shown a reason, so it says what actually forced it.
		reason := fmt.Sprintf("%s container is not supported, but both codecs are", r.Container)
		if alternateAudio && contains(p.Containers, r.Container) {
			reason = "a different audio track was chosen, which direct play cannot select"
		}
		return Decision{
			Method:      Remux,
			Reason:      reason,
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
		d := Decision{
			Method: Transcode, Reason: reason,
			VideoAction: "encode", AudioAction: audioAction, TargetFormat: "mp4",
		}
		// A ceiling only becomes a target when the source is actually above it.
		// Carrying it down unconditionally would tell ffmpeg to scale a 720p
		// file up to 1080p because the client said "1080p is my limit" — a
		// limit read as a request, paying for an encode that adds no detail and
		// costs bandwidth. Same for bitrate: capping a 3 Mbps file at 8 Mbps is
		// a rate control constraint that can only ever be slack.
		if video != nil {
			// Every encode this package produces is 8-bit H.264 SDR, so an HDR
			// source being re-encoded is always an HDR-to-SDR conversion. There
			// is no configuration in which it is not (ADR 0033).
			d.TonemapHDR = IsHDR(video)

			d.SourceWidth, d.SourceHeight = video.Width, video.Height
			// Already normalised to a decimal string by the probe; an
			// unparseable one stays zero rather than becoming a guess.
			if f, err := strconv.ParseFloat(video.FrameRate, 64); err == nil {
				d.SourceFrameRate = f
			}

			if p.MaxHeight > 0 && video.Height > p.MaxHeight {
				d.TargetHeight = p.MaxHeight
			}
			if p.MaxVideoBitRate > 0 &&
				(video.BitRate == 0 || video.BitRate > p.MaxVideoBitRate) {
				// An unknown source bitrate is capped rather than left open:
				// a file the probe could not measure is exactly the case where
				// a cap is worth having, and rate control that does nothing is
				// indistinguishable from a quality setting that does nothing.
				d.TargetVideoBitRate = p.MaxVideoBitRate
			}
		}
		return d
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
func mp4CarriesAudio(s *Stream, p Profile) bool {
	if s == nil {
		return true
	}
	switch strings.ToLower(s.Codec) {
	case "aac", "mp3", "ac3", "eac3":
		return true
	/*
	 * The lossless cases, added because leaving them out was destroying data.
	 *
	 * A codec absent from this list cannot be copied, so a file needing only a
	 * *container* rewrite gets its audio re-encoded — and for FLAC and ALAC
	 * that means lossless becoming AAC to change a box the client did not like.
	 *
	 * Carriage is verified rather than assumed. On ffmpeg 8.1.2, with this
	 * package's own flags, all three copy into fragmented MP4 with exit 0 and
	 * no stderr, carrying the standard box tags `fLaC`, `Opus` and `alac`.
	 *
	 * Playing them is a separate question and the reason FLAC and Opus are
	 * gated on a claim: Chromium reports isTypeSupported true for both in MP4
	 * and reaches readyState 4 on real files, and Chromium is not every
	 * browser. The floor stays conservative and the client that can answer for
	 * the exact MP4 codec string gets its lossless kept.
	 *
	 * Vorbis is deliberately absent. It has no settled mapping into MP4, and
	 * every Vorbis file measured was in Ogg — which the browser takes as it
	 * stands — so carrying it would be a risk run for no file that exists.
	 */
	case "alac":
		return true
	case "flac":
		return p.Allows("flacmp4")
	case "opus":
		return p.Allows("opusmp4")
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
	/*
	 * 10-bit H.264 is a real trap: the codec name matches, browsers advertise
	 * H.264 support, and playback still fails. High 10 is in no browser's
	 * baseline, so it stays out of every profile and needs asking for by name.
	 *
	 * Unlike `hevc10`, the check is *not* scoped to claims. H.264 is listed
	 * natively by every profile here including `tv`, and trusting a native
	 * listing the way HEVC's does would hand High 10 to set-top boxes on the
	 * strength of them decoding 8-bit H.264 — which is the same conflation
	 * hevc10 exists to undo, and wrong more often, since High 10 is missing
	 * from most fixed-function decoders that manage High profile perfectly.
	 *
	 * So every client asks, and the answer is worth having: Chromium reports
	 * `probably` for `avc1.6e0033`, and High 10 is most of an anime library —
	 * a full video re-encode, every time, on files it can play.
	 */
	if strings.EqualFold(s.Codec, "h264") && isTenBit(s) && !p.Allows("high10") {
		return false, "10-bit H.264 needs a decoder this client did not claim"
	}
	/*
	 * 10-bit HEVC is the same trap one codec along, and it took a real film to
	 * find it: Main 10 direct-played with perfect audio and a stuttering
	 * picture, because "hevc" had been claimed from an 8-bit `hvc1.1.6` probe
	 * and read as covering Main 10 too.
	 *
	 * A *claim* of "hevc" now means 8-bit; ten-bit needs `hevc10`, which the
	 * client only sends when the engine answered for Main 10 specifically.
	 *
	 * Scoped to claims deliberately. A profile that lists HEVC natively — `tv`,
	 * `safari` — is a device class known to decode Main 10 in hardware, and
	 * demanding a claim from it would re-encode HDR for exactly the clients that
	 * handle it best. The distrust belongs to the guess, not to the profile.
	 *
	 * Worth noting why the existing safety net did not catch it: a failed
	 * direct play records the claim and stops making it, but this did not fail.
	 * It played. Badly. Nothing in the system can see the difference between a
	 * smooth picture and a glitching one, which is why the question has to be
	 * asked accurately rather than recovered from afterwards.
	 */
	if strings.EqualFold(s.Codec, "hevc") && isTenBit(s) &&
		p.Allows("hevc") && !p.Allows("hevc10") {
		return false, "10-bit HEVC needs a decoder this client did not claim"
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

/*
 * IsHDR reports whether a video stream carries high dynamic range.
 *
 * Defined by the **transfer function**, and deliberately by nothing else
 * (ADR 0033).
 *
 * Bit depth is the trap. 10-bit is correlated with HDR and does not mean it:
 * `yuv420p10le` is what HDR10 reports and it is also what 10-bit SDR reports,
 * and a 10-bit SDR file put through a tone map would be damaged by it exactly
 * as surely as an HDR file is damaged by not being.
 *
 * Primaries are the weaker signal from the other direction. BT.2020 primaries
 * turn up on SDR content occasionally, and it is the transfer curve that
 * actually decides whether the code values need converting rather than merely
 * reinterpreting.
 *
 * The two values are the two HDR systems in circulation: `smpte2084` is PQ,
 * which is HDR10 and the base layer Dolby Vision sits on top of, and
 * `arib-std-b67` is HLG. Dolby Vision profile 5 is *not* PQ, will not match
 * here, and is treated as SDR — which is what happens today, and is a much
 * larger piece of work to do properly.
 *
 * A stream with no recorded transfer is not HDR. Every row predates the probe
 * change that records it until re-probed, and guessing HDR from a blank field
 * would tone map an entire library on no evidence.
 */
func IsHDR(s *Stream) bool {
	if s == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s.ColorTransfer)) {
	case "smpte2084", "arib-std-b67":
		return true
	}
	return false
}
