package store

import "testing"

/*
 * Which track plays, and why.
 *
 * Modelled on internal/probe/decide_test.go: one named case per situation, each
 * asserting the decision *and* its reason. A choice with no reason cannot be
 * told apart from a bug, and every case below is a shape a real library holds.
 *
 * The two that matter most are the ones where doing something is worse than
 * doing nothing — a language that is absent, and subtitles over a film somebody
 * can already understand.
 */

func audio(index int, lang string, isDefault bool) MediaStream {
	return MediaStream{Index: index, Kind: "audio", Language: lang, Default: isDefault}
}

func subs(index int, lang string, forced bool) MediaStream {
	return MediaStream{Index: index, Kind: "subtitle", Language: lang, Forced: forced}
}

func gotIndex(p *int) string {
	if p == nil {
		return "none"
	}
	return string(rune('0' + *p))
}

func TestNoPreferenceChangesNothing(t *testing.T) {
	/*
	 * Every account that existed before this feature has empty preferences, and
	 * must behave exactly as it did. A nil audio index is the file's own default
	 * playing, which is what happened before.
	 */
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), audio(1, "eng", false),
	}, LanguagePrefs{})
	if got.AudioIndex != nil || got.SubtitleIndex != nil {
		t.Errorf("chose %+v, want nothing touched", got)
	}
}

func TestThePreferredAudioTrackIsChosen(t *testing.T) {
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), audio(1, "eng", false),
	}, LanguagePrefs{Audio: "en"})
	if got.AudioIndex == nil || *got.AudioIndex != 1 {
		t.Fatalf("audio = %s, want track 1", gotIndex(got.AudioIndex))
	}
	if got.Why == "" {
		t.Error("chose a track and gave no reason")
	}
}

func TestTwoLetterAndThreeLetterCodesAreTheSameLanguage(t *testing.T) {
	/*
	 * A file says `eng`, a person picks `en`, and a muxer somewhere says
	 * `en-US`. All three are English, and a preference that matched only its
	 * own spelling would look broken on half a library.
	 */
	for _, want := range []string{"en", "eng", "EN", "en-GB"} {
		for _, have := range []string{"eng", "en", "en-US", "en_GB"} {
			got := ChooseTracks([]MediaStream{
				audio(0, "jpn", true), audio(1, have, false),
			}, LanguagePrefs{Audio: want})
			if got.AudioIndex == nil || *got.AudioIndex != 1 {
				t.Errorf("want %q against %q: audio = %s", want, have, gotIndex(got.AudioIndex))
			}
		}
	}
}

func TestAnAbsentLanguageLeavesTheFileAlone(t *testing.T) {
	/*
	 * The rule that makes this safe to switch on. Choosing "the first track"
	 * for somebody whose language is not in the file would override the file's
	 * own default for no reason — on a library of foreign films that is every
	 * film, and it would look like LANcast picking at random.
	 */
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), audio(1, "fra", false),
	}, LanguagePrefs{Audio: "en"})
	if got.AudioIndex != nil {
		t.Errorf("chose track %s; nothing was in the preferred language", gotIndex(got.AudioIndex))
	}
	if got.Why == "" {
		t.Error("left the audio alone and gave no reason")
	}
}

func TestTheFilmWinsOverItsCommentary(t *testing.T) {
	/*
	 * A disc rip routinely carries an English track and an English director's
	 * commentary, and the commentary is often the later one. "First match"
	 * plays the commentary for anybody who set a preference, which is a worse
	 * outcome than not having the feature at all.
	 *
	 * The file marks the film as default, so the file's own answer breaks the
	 * tie.
	 */
	got := ChooseTracks([]MediaStream{
		audio(0, "eng", false), // commentary, listed first
		audio(1, "eng", true),  // the film
	}, LanguagePrefs{Audio: "en"})
	if got.AudioIndex == nil || *got.AudioIndex != 1 {
		t.Errorf("audio = %s, want the default-marked English track", gotIndex(got.AudioIndex))
	}
}

func TestUndeterminedIsNotALanguage(t *testing.T) {
	// ffprobe writes `und` when a muxer said nothing. Treating it as a language
	// would let it match another `und` and pick an arbitrary track as though it
	// were a deliberate answer.
	got := ChooseTracks([]MediaStream{audio(0, "und", true), audio(1, "und", false)},
		LanguagePrefs{Audio: "und"})
	if got.AudioIndex != nil {
		t.Errorf("matched `und` as a language, choosing %s", gotIndex(got.AudioIndex))
	}
}

func TestSubtitlesStayOffUnlessAskedFor(t *testing.T) {
	// The default mode. An account that set an audio language and nothing else
	// gets no subtitles, which is what most people mean by setting one.
	got := ChooseTracks([]MediaStream{
		audio(0, "eng", true), subs(1, "eng", false),
	}, LanguagePrefs{Audio: "en", Subtitle: "en"})
	if got.SubtitleIndex != nil {
		t.Errorf("turned subtitles on with no mode set: %+v", got)
	}
}

func TestForeignModeIsQuietWhenTheAudioWasUnderstood(t *testing.T) {
	/*
	 * The case that decides whether this feature is kept or switched off. A
	 * film with an English track that *was* chosen needs no subtitles. Reading
	 * "foreign" off the file instead — "this is a Japanese film" — puts
	 * subtitles over the English dub somebody deliberately selected.
	 */
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), audio(1, "eng", false), subs(2, "eng", false),
	}, LanguagePrefs{Audio: "en", Subtitle: "en", Mode: SubtitleForeign})
	if got.AudioIndex == nil || *got.AudioIndex != 1 {
		t.Fatalf("audio = %s, want the English track", gotIndex(got.AudioIndex))
	}
	if got.SubtitleIndex != nil {
		t.Errorf("subtitles on over audio the account can understand: %+v", got)
	}
}

func TestForeignModeFiresWhenThereIsNoTrackToUnderstand(t *testing.T) {
	// The other half: no English audio, so the subtitles come on without being
	// asked for — which is the whole point of the mode.
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), subs(1, "eng", false),
	}, LanguagePrefs{Audio: "en", Subtitle: "en", Mode: SubtitleForeign})
	if got.SubtitleIndex == nil || *got.SubtitleIndex != 1 {
		t.Fatalf("subtitle = %s, want track 1", gotIndex(got.SubtitleIndex))
	}
	if got.Why == "" {
		t.Error("turned subtitles on and gave no reason")
	}
}

func TestForeignModeWithNoAudioPreferenceDoesNothing(t *testing.T) {
	/*
	 * With no audio preference there is nothing for the audio to be *foreign
	 * to*. Firing anyway would mean guessing what the account considers its own
	 * language, which is exactly the guess this design refuses to make.
	 */
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), subs(1, "eng", false),
	}, LanguagePrefs{Subtitle: "en", Mode: SubtitleForeign})
	if got.SubtitleIndex != nil {
		t.Errorf("fired with nothing to be foreign to: %+v", got)
	}
}

func TestAlwaysModeShowsThemRegardless(t *testing.T) {
	// For a household that watches with subtitles on principle, including over
	// audio it understands perfectly well.
	got := ChooseTracks([]MediaStream{
		audio(0, "eng", true), subs(1, "eng", false),
	}, LanguagePrefs{Audio: "en", Subtitle: "en", Mode: SubtitleAlways})
	if got.SubtitleIndex == nil || *got.SubtitleIndex != 1 {
		t.Fatalf("subtitle = %s, want track 1", gotIndex(got.SubtitleIndex))
	}
	if got.Why != "subtitles are always on for this account" {
		t.Errorf("why = %q", got.Why)
	}
}

func TestAFullTrackIsPreferredOverAForcedOne(t *testing.T) {
	/*
	 * Forced subtitles translate the foreign lines inside an otherwise English
	 * film. Somebody who has them on because the audio is foreign needs the
	 * full track; a forced one would caption three sentences of an entire film
	 * and look like the feature failing.
	 */
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), subs(1, "eng", true), subs(2, "eng", false),
	}, LanguagePrefs{Audio: "en", Subtitle: "en", Mode: SubtitleForeign})
	if got.SubtitleIndex == nil || *got.SubtitleIndex != 2 {
		t.Errorf("subtitle = %s, want the full track", gotIndex(got.SubtitleIndex))
	}
}

func TestAForcedTrackIsTakenWhenItIsTheOnlyOne(t *testing.T) {
	// Better than nothing, and common on films that ship only a forced track.
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), subs(1, "eng", true),
	}, LanguagePrefs{Audio: "en", Subtitle: "en", Mode: SubtitleForeign})
	if got.SubtitleIndex == nil || *got.SubtitleIndex != 1 {
		t.Errorf("subtitle = %s, want the forced track", gotIndex(got.SubtitleIndex))
	}
}

func TestNoSubtitleInTheWantedLanguageIsSilent(t *testing.T) {
	// Asking for English subtitles over a file that has only French ones is a
	// request that cannot be met. Showing the French is worse than showing
	// none.
	got := ChooseTracks([]MediaStream{
		audio(0, "jpn", true), subs(1, "fra", false),
	}, LanguagePrefs{Audio: "en", Subtitle: "en", Mode: SubtitleForeign})
	if got.SubtitleIndex != nil {
		t.Errorf("chose a subtitle in the wrong language: %+v", got)
	}
}

/*
 * The comparison itself.
 */

func TestLangMatches(t *testing.T) {
	cases := []struct {
		want, got string
		match     bool
	}{
		{"en", "eng", true},
		{"eng", "en", true},
		{"en", "en-US", true},
		{"pt", "pt_BR", true},
		// Portuguese is Portuguese: somebody who asked for it would rather hear
		// the Brazilian track than nothing.
		{"pt-PT", "pt-BR", true},
		{"en", "fra", false},
		{"en", "", false},
		{"", "eng", false},
		{"en", "und", false},
		{"und", "und", false},
	}
	for _, tc := range cases {
		if got := LangMatches(tc.want, tc.got); got != tc.match {
			t.Errorf("LangMatches(%q, %q) = %v, want %v", tc.want, tc.got, got, tc.match)
		}
	}
}

func TestNormalizeLang(t *testing.T) {
	for _, tc := range []struct {
		in, out string
		ok      bool
	}{
		{"EN", "en", true},
		{" eng ", "eng", true},
		{"", "", true}, // no preference is a valid answer
		{"e", "", false},
		{"engl", "", false},
		{"e1", "", false},
		{"en-US", "", false}, // a region belongs to a file, not to a preference
	} {
		got, ok := NormalizeLang(tc.in)
		if got != tc.out || ok != tc.ok {
			t.Errorf("NormalizeLang(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.out, tc.ok)
		}
	}
}
