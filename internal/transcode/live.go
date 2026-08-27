package transcode

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"lancast/internal/probe"
)

/*
 * Live channels through ffmpeg.
 *
 * The reason this exists at all: most IPTV channels are HLS carrying MPEG-TS,
 * and Chromium decodes neither — `canPlayType("video/mp2t")` is an empty
 * string, and ADR 0013 refuses to vendor hls.js. Relaying a channel untouched
 * therefore works in Safari and nowhere else, which is not a feature.
 *
 * What makes this cheap enough to do per viewer is that it is usually **not a
 * transcode**. Nearly every channel is H.264 video with AAC audio, which fMP4
 * accepts as-is: ffmpeg rewrites the container and copies both streams. That
 * costs a few percent of one core rather than a whole one, and it is the same
 * remux decision the file path already makes — reused here rather than
 * re-derived, because a second copy of the codec rules would eventually
 * disagree with the first.
 */

// LiveOptions describe one channel stream.
type LiveOptions struct {
	// URL is the upstream address. It never reaches a client; only ffmpeg and
	// this process ever see it.
	URL string
	// Decision comes from probing the channel. A zero Decision means "copy
	// everything", which is the right default for the overwhelmingly common
	// case and the cheapest thing that can be tried.
	Decision probe.Decision
	/*
	 * HLS marks the source as an HLS playlist, which decides where ffmpeg
	 * starts reading it.
	 *
	 * False when the channel could not be probed, which is the safe direction:
	 * `-live_start_index` belongs to the HLS demuxer and makes ffmpeg refuse a
	 * plain transport stream outright, so an unprobed channel keeps the
	 * behaviour it has always had rather than risking a dead one.
	 */
	HLS bool
}

// IsHLS reports whether a probe found an HLS playlist. Nil-safe, because a
// failed probe is ordinary on a live source and every caller would otherwise
// repeat the same check.
func IsHLS(r *probe.Result) bool {
	return r != nil && strings.EqualFold(r.Container, "hls")
}

/*
 * Live starts ffmpeg on a channel and returns its output as fMP4.
 *
 * The returned reader must be closed, and closing it kills ffmpeg. That is the
 * whole lifetime story and it is the part that matters: a live source never
 * ends, so nothing else will ever stop the process. A leaked session here does
 * not idle — it pulls a stream at full rate, for ever, for a viewer who closed
 * the tab an hour ago.
 *
 * The caller is therefore expected to tie ctx to the HTTP request, which is
 * what makes a closed tab stop the encode.
 */
func (m *Manager) Live(ctx context.Context, channelID int64, o LiveOptions) (io.ReadCloser, error) {
	if !m.Available() {
		return nil, ErrNotInstalled
	}
	if err := m.reserve(); err != nil {
		return nil, err
	}

	decision := o.Decision
	if decision.VideoAction == "" {
		// Copy is both the cheap answer and the honest default: nothing has
		// told us the streams need changing, and re-encoding on suspicion
		// would spend a core per viewer to solve a problem that usually does
		// not exist.
		decision.VideoAction = "copy"
	}
	if decision.AudioAction == "" {
		decision.AudioAction = "copy"
	}

	opts := Options{
		Input:      o.URL,
		Output:     Progressive,
		Live:       true,
		HLSInput:   o.HLS,
		Decision:   decision,
		AudioIndex: -1,
		Encoder:    m.Encoder(),
	}

	s, stdout, err := startProgressive(ctx, m.binary(), opts)
	if err != nil {
		m.release()
		return nil, err
	}
	// Channel ids and item ids are different numbering, so this is recorded
	// negated: a session list that mixed them would show a channel as though it
	// were a library item, and the two are not interchangeable anywhere else in
	// the system.
	s.ID, s.ItemID = newID(), -channelID

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()

	m.log.Info("live transcode started", "session", s.ID, "channel", channelID,
		"video", decision.VideoAction, "audio", decision.AudioAction)

	return &sessionReader{ReadCloser: stdout, m: m, s: s}, nil
}

/*
 * LiveHLS returns a session producing HLS for this channel, starting one if
 * needed.
 *
 * Unlike the progressive path, this is **shared between viewers**. A
 * progressive stream is a pipe into one response and cannot be anything else,
 * which is why `Progressive` supersedes rather than reuses. Segments are files:
 * two people watching one channel read the same ones, so a second viewer costs
 * an HTTP handler rather than an ffmpeg. On a bounded `MaxSessions` that is the
 * difference between a channel two people can watch and one they cannot.
 *
 * Keyed on the channel alone for the same reason — there is no per-viewer state
 * in a live stream to keep apart. There is no offset either: a channel has one
 * position, now, so the `sameOffset` reuse test the file path needs does not
 * apply and is not consulted.
 */
func (m *Manager) LiveHLS(ctx context.Context, channelID int64, o LiveOptions) (*Session, error) {
	if !m.Available() {
		return nil, ErrNotInstalled
	}

	// Channel ids are recorded negated, as in Live, because channel and item
	// numbering are different and a session list that mixed them would show a
	// channel as though it were a library item.
	key := -channelID

	m.mu.Lock()
	for _, s := range m.sessions {
		if s.ItemID == key && s.Output == HLS {
			s.Touch()
			m.mu.Unlock()
			return s, nil
		}
	}
	m.mu.Unlock()

	if err := m.reserve(); err != nil {
		return nil, err
	}

	decision := o.Decision
	if decision.VideoAction == "" {
		decision.VideoAction = "copy"
	}
	if decision.AudioAction == "" {
		decision.AudioAction = "copy"
	}

	id := newID()
	opts := Options{
		Input:      o.URL,
		Output:     HLS,
		Live:       true,
		HLSInput:   o.HLS,
		Decision:   decision,
		AudioIndex: -1,
		Encoder:    m.Encoder(),
		OutputDir:  filepath.Join(m.root, id),
	}
	opts.CanTonemap, opts.CanTagSDR = m.colourFor()

	s, err := startHLS(ctx, m.binary(), opts)
	if err != nil {
		m.release()
		return nil, err
	}
	s.ID, s.ItemID = id, key

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	m.log.Info("live transcode started", "session", id, "channel", channelID,
		"output", "hls", "video", decision.VideoAction, "audio", decision.AudioAction)

	return s, nil
}

// LiveDecision turns a probe of a channel into the actions ffmpeg should take.
//
// Separated from the request so the rules are testable without a live source,
// and thin on purpose: the codec rules belong to probe.Decide, which the file
// path has been using since M3 and which has a test per codec combination.
func LiveDecision(r *probe.Result, p probe.Profile, log *slog.Logger) probe.Decision {
	if r == nil {
		/*
		 * Nothing was learned about the stream — the probe failed, timed out,
		 * or ffprobe is absent.
		 *
		 * Video is copied, because re-encoding on suspicion spends a core per
		 * viewer to fix a problem that usually does not exist. Audio is
		 * **encoded**, because the opposite is true there: it costs a few
		 * percent, and copying an unknown codec risks a channel that shows a
		 * picture and plays nothing — a failure that looks like success.
		 */
		return probe.Decision{
			Method:      probe.Transcode,
			Reason:      "channel was not probed; video copied, audio re-encoded",
			VideoAction: "copy",
			AudioAction: "encode",
		}
	}
	d := probe.Decide(r, p)

	/*
	 * Audio is copied only when it is known to be AAC.
	 *
	 * The bitstream filter above converts ADTS framing to what MP4 wants, and
	 * it is specific to AAC — pointing it at AC-3 or MP2 fails the mux. Those
	 * codecs also cannot play in a browser, so copying them would produce a
	 * working picture with silence, which is the worst outcome available
	 * because it looks like it nearly worked.
	 *
	 * Re-encoding audio is cheap — a few percent of a core, against the whole
	 * core a video encode costs — so the rule is: copy video whenever possible,
	 * and only copy audio when it is already the thing MP4 and the browser both
	 * want.
	 */
	if d.AudioAction == "copy" && !isAAC(r) {
		d.AudioAction = "encode"
		d.Reason = "audio re-encoded to AAC for the browser; " + d.Reason
	}
	// Direct play is not available for a channel: the whole point of this path
	// is that the browser could not take the source as it stands, so the
	// container is being rewritten whatever the codecs say.
	if d.Method == probe.DirectPlay {
		d.Method = probe.Remux
		d.Reason = "container rewritten for the browser; streams copied"
	}
	if log != nil {
		log.Debug("live decision", "method", d.Method,
			"video", d.VideoAction, "audio", d.AudioAction, "reason", d.Reason)
	}
	return d
}

// isAAC reports whether the source's audio is AAC, which is the only codec MP4
// and every browser both accept on a copy.
func isAAC(r *probe.Result) bool {
	if r == nil {
		return false
	}
	for _, st := range r.Streams {
		if st.Kind == probe.KindAudio {
			return strings.EqualFold(st.Codec, "aac")
		}
	}
	return false
}
