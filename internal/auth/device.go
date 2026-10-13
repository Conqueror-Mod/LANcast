package auth

import "strings"

/*
 * DeviceName says what signed in, in the words a person would use for it:
 * "LANcast app on Windows", "Chrome on Android", "Safari on iPhone".
 *
 * For Your account › Security › Authorized devices (docs/settings-plan.md,
 * step 4), where the question is "is that one of mine?". A user agent is a
 * claim, not a proof, so this is a label and never a decision: nothing is
 * allowed or refused on it.
 *
 * Order matters. Edge's user agent also says Chrome and Safari, Chrome's
 * also says Safari, and the LANcast window's says Edge — so the most specific
 * name is tested first.
 */
func DeviceName(ua string) string {
	if ua == "" {
		return "Unknown device"
	}
	app := ""
	switch {
	case strings.Contains(ua, "LANcast-Desktop/"):
		app = "LANcast app"
	case strings.Contains(ua, "Edg/") || strings.Contains(ua, "EdgA/") || strings.Contains(ua, "EdgiOS/"):
		app = "Edge"
	case strings.Contains(ua, "OPR/"):
		app = "Opera"
	case strings.Contains(ua, "Firefox/") || strings.Contains(ua, "FxiOS/"):
		app = "Firefox"
	case strings.Contains(ua, "SamsungBrowser/"):
		app = "Samsung Internet"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		app = "Chrome"
	case strings.Contains(ua, "Safari/"):
		app = "Safari"
	case strings.HasPrefix(ua, "curl/"):
		return "curl"
	default:
		app = "A browser"
	}
	osName := ""
	switch {
	case strings.Contains(ua, "iPhone"):
		osName = "iPhone"
	case strings.Contains(ua, "iPad"):
		osName = "iPad"
	case strings.Contains(ua, "Android"):
		osName = "Android"
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		osName = "Mac"
	case strings.Contains(ua, "CrOS"):
		osName = "Chromebook"
	case strings.Contains(ua, "Linux"):
		osName = "Linux"
	}
	if osName == "" {
		return app
	}
	return app + " on " + osName
}
