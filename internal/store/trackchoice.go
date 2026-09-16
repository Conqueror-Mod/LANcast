package store

/*
 * Which track to start with.
 *
 * Pure, and taking streams rather than an item id, for the reason
 * probe.ParseJSON is pure: every rule below is a case somebody's library
 * actually contains, and all of them are testable in milliseconds with no
 * media, no ffmpeg and no database.
 *
 * It lives in store rather than in the client because the *preference* does,
 * and a second implementation in TypeScript would be a second opinion about
 * what "English" means — the one-normalizer rule, applied to languages. The
 * client asks for an answer and renders it.
 */

// TrackChoice is which audio track to play and which subtitle to show.
//
// A nil index means "no opinion": play whatever the file marks default, which
// is exactly what happened before preferences existed. Distinct from an index
// of 0, which is a deliberate choice of the first track.
type TrackChoice struct {
	// AudioIndex is the chosen stream's index, or nil for the file's default.
	AudioIndex *int `json:"audio_index,omitempty"`
	// SubtitleIndex is the chosen subtitle stream, or nil for none.
	SubtitleIndex *int `json:"subtitle_index,omitempty"`
	/*
	 * Why is what the client shows when it explains itself, and what a test
	 * asserts on. A choice with no reason is indistinguishable from a bug —
	 * the decision rules in internal/probe are the model: assert the decision
	 * *and* its reason.
	 */
	Why string `json:"why,omitempty"`
}

// LanguagePrefs is what an account asked for.
type LanguagePrefs struct {
	Audio    string
	Subtitle string
	Mode     string
}

/*
 * ChooseTracks picks the audio and subtitle streams for one playback.
 *
 * The rules, in order, each of which is a real case:
 *
 *  1. No preference at all leaves everything alone. An account that has never
 *     set one must behave exactly as it did before this existed.
 *  2. An audio track in the preferred language wins, preferring the one the
 *     file marks default where several match — a film with an English track
 *     and an English commentary track should play the film.
 *  3. No match leaves the audio alone rather than picking something. Choosing
 *     "the first track" for somebody whose language is absent would override
 *     the file's own default for no reason.
 *  4. Subtitles follow the mode, and `foreign` is judged on what *plays*
 *     rather than on what the file holds.
 *
 * Forced subtitles are deliberately not special-cased here — see below.
 */
func ChooseTracks(streams []MediaStream, prefs LanguagePrefs) TrackChoice {
	if prefs.Audio == "" && prefs.Subtitle == "" {
		return TrackChoice{Why: "no language preference is set"}
	}

	var choice TrackChoice
	spokeTheLanguage := false

	if prefs.Audio != "" {
		if i, ok := pickAudio(streams, prefs.Audio); ok {
			choice.AudioIndex = &i
			spokeTheLanguage = true
			choice.Why = "an audio track in the preferred language"
		} else {
			/*
			 * Nothing matched. The file's own default plays, and the subtitle
			 * decision below has to know that the audio is *not* in the wanted
			 * language — which is the whole of what `foreign` means.
			 */
			choice.Why = "no audio track in the preferred language; the file's default plays"
		}
	}
	mode := prefs.Mode
	if mode == "" {
		mode = SubtitleOff
	}
	if mode == SubtitleOff || prefs.Subtitle == "" {
		return choice
	}

	/*
	 * `foreign` means "when I cannot understand what is being said".
	 *
	 * So it turns subtitles on exactly when the preferred audio was *not*
	 * found. Reading it off the file instead — "this is a foreign film" — puts
	 * subtitles over a film whose English dub is playing, which is the version
	 * of this feature people switch off.
	 *
	 * With no audio preference set there is nothing to be foreign *to*, so the
	 * mode cannot fire and the account is left alone rather than guessed at.
	 */
	if mode == SubtitleForeign && (prefs.Audio == "" || spokeTheLanguage) {
		return choice
	}

	if i, ok := pickSubtitle(streams, prefs.Subtitle); ok {
		choice.SubtitleIndex = &i
		switch mode {
		case SubtitleAlways:
			choice.Why = "subtitles are always on for this account"
		default:
			choice.Why = "the audio is not in the preferred language"
		}
	}
	return choice
}

/*
 * pickAudio finds the best audio track in a language.
 *
 * "Best" is the file's own default where more than one matches. A disc rip
 * routinely carries an English track and an English director's commentary, and
 * the commentary is often the later one — so "first match" plays the commentary
 * for anybody who set a preference, which is a worse outcome than not having
 * the feature.
 */
func pickAudio(streams []MediaStream, want string) (index int, ok bool) {
	first := -1
	for _, st := range streams {
		if st.Kind != "audio" || !LangMatches(want, st.Language) {
			continue
		}
		if st.Default {
			return st.Index, true
		}
		if first < 0 {
			first = st.Index
		}
	}
	if first < 0 {
		return 0, false
	}
	return first, true
}

/*
 * pickSubtitle finds the subtitle track to show.
 *
 * A **forced** track is preferred over a full one when both are in the wanted
 * language, and this is the one place a flag is read rather than a language.
 * Forced subtitles translate the foreign lines inside an otherwise English
 * film — the Elvish in a fantasy film — and somebody who asked for subtitles
 * because the audio is foreign wants the full track, while somebody watching
 * with subtitles always on still wants the forced one to win where it exists,
 * because the full track duplicates dialogue they can already hear.
 *
 * That distinction is not modelled yet and is deliberately left alone: making
 * it would mean guessing which of the two a person meant, and guessing wrong
 * is worse than one consistent answer. The full track wins, and a forced one
 * is only taken when it is the only match.
 */
func pickSubtitle(streams []MediaStream, want string) (int, bool) {
	forced := -1
	for _, st := range streams {
		if st.Kind != "subtitle" || !LangMatches(want, st.Language) {
			continue
		}
		if st.Forced {
			if forced < 0 {
				forced = st.Index
			}
			continue
		}
		return st.Index, true
	}
	if forced >= 0 {
		return forced, true
	}
	return 0, false
}
