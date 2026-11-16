package transcode

/*
 * workKey is what a session was asked to make, as far as reusing it goes.
 *
 * EnsureHLS reuses a viewer's session for the same item and offset, so that a
 * player asking for its playlist again does not start an ffmpeg per request.
 * That match never looked at the work itself. A viewer who lowered Quality
 * before the film had moved asked again at the same offset, and was handed the
 * session already running at the old quality: measured on the installed
 * service, a 1036p film with AC3 audio was decided as a 720p encode, served
 * the earlier copied-video remux instead, and waited 1m42s for that remux to
 * finish — where a real encode lists its playlist up front and starts at once.
 * Choosing another audio track before playback moved went the same way.
 *
 * So a session is reused only for the same work. Anything else falls through
 * to supersedeHLS, which stops this viewer's old session for the item, exactly
 * as a seek does.
 *
 * The decision's Reason is left out on purpose: it is words about the work,
 * and two decisions that would run the same ffmpeg command are the same work.
 */
type workKey struct {
	videoAction        string
	audioAction        string
	targetFormat       string
	audioOnly          bool
	targetHeight       int
	targetVideoBitRate int64
	tonemapHDR         bool
	audioIndex         int
}

func workFor(o Options) workKey {
	d := o.Decision
	return workKey{
		videoAction:        d.VideoAction,
		audioAction:        d.AudioAction,
		targetFormat:       d.TargetFormat,
		audioOnly:          d.AudioOnly,
		targetHeight:       d.TargetHeight,
		targetVideoBitRate: d.TargetVideoBitRate,
		tonemapHDR:         d.TonemapHDR,
		audioIndex:         o.AudioIndex,
	}
}
