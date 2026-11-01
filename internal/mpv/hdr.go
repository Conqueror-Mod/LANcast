package mpv

/*
 * HDR to the display (docs/theater-output-plan.md, step 2).
 *
 * With target-colorspace-hint on, gpu-next tells the swapchain the film's own
 * colour space, so an HDR10 film reaches an HDR display as HDR rather than
 * being tone mapped down to SDR inside mpv. Off is what LANcast has always
 * done, and stays the default.
 *
 * mpv can ask for HDR; it cannot turn on Windows' own "Use HDR" for a screen.
 * Without that the desktop stays SDR and Windows maps the picture down itself,
 * which is why the player logs the display's state next to the film's
 * (VideoReport) instead of assuming.
 */

// HDRHint is the target-colorspace-hint value for the setting.
func HDRHint(on bool) string {
	if on {
		return "yes"
	}
	return "no"
}

// VideoProps are the properties VideoReport reads, by mpv's names.
var VideoProps = []string{
	"video-params/gamma", "video-params/primaries",
	"video-target-params/gamma", "video-target-params/primaries",
}

// VideoReport is what one film's picture was, and what mpv sent the display.
type VideoReport struct {
	FilmGamma, FilmPrimaries     string
	TargetGamma, TargetPrimaries string
}

// hdrGammas are the transfer functions that mean a picture is HDR.
var hdrGammas = map[string]bool{"pq": true, "hlg": true}

// FilmHDR reports whether the film itself is HDR (PQ or HLG).
func (r VideoReport) FilmHDR() bool { return hdrGammas[r.FilmGamma] }

// TargetHDR reports whether mpv is sending the display an HDR signal.
func (r VideoReport) TargetHDR() bool { return hdrGammas[r.TargetGamma] }

/*
 * Verdict says, in one phrase for the log, what happened to the picture.
 *
 * The cases are the ones somebody would ask about: "why isn't my TV showing
 * HDR?" has three different answers, and each wants a different fix.
 */
func (r VideoReport) Verdict(hint, displayHDR bool) string {
	switch {
	case !r.FilmHDR():
		return "SDR film, shown as SDR"
	case r.TargetHDR():
		return "HDR film sent to the display as HDR"
	case !hint:
		return "HDR film tone mapped to SDR (Send HDR to the display is off)"
	case !displayHDR:
		return "HDR film tone mapped to SDR (Use HDR is off for this screen in Windows)"
	default:
		return "HDR film tone mapped to SDR, though the hint is on and the screen is in HDR"
	}
}
