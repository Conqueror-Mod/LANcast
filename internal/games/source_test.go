package games

import "testing"

/*
 * Ids, and the upgrade that must not lose anybody's hidden games.
 *
 * Before three readers existed an id was a bare Steam appid. Hidden and
 * favourite flags are stored by id in a per-client JSON file that nothing
 * migrates — so an id this build cannot read is indistinguishable from a game
 * that has been uninstalled, and the flags on it quietly stop applying.
 *
 * That is the whole reason SplitID treats an unprefixed id as Steam's.
 */

func TestABareIdIsAStreamAppidFromBeforeNamespacing(t *testing.T) {
	source, own, ok := SplitID("440")
	if !ok {
		t.Fatal("a bare appid was rejected; every stored flag from before this change would stop applying")
	}
	if source != SourceSteam || own != "440" {
		t.Errorf("got %q/%q, want steam/440", source, own)
	}
}

func TestEachLauncherRoundTrips(t *testing.T) {
	cases := []struct {
		id     string
		source Source
		own    string
	}{
		{SteamID("440"), SourceSteam, "440"},
		{EpicID("a26f991a5e6c4e9c9572fc200cbea47f"), SourceEpic, "a26f991a5e6c4e9c9572fc200cbea47f"},
		{BattleNetID("Hearthstone"), SourceBattleNet, "Hearthstone"},
	}
	for _, tc := range cases {
		source, own, ok := SplitID(tc.id)
		if !ok || source != tc.source || own != tc.own {
			t.Errorf("%q split to %q/%q/%v", tc.id, source, own, ok)
		}
	}
}

func TestAKeyContainingAColonStillSplitsAtTheFirstOne(t *testing.T) {
	/*
	 * A registry key or a display name can contain anything. Cutting at the
	 * first colon rather than the last is what keeps the rest of the id intact
	 * — and the rest is what gets compared against a rescan.
	 */
	source, own, ok := SplitID("battlenet:Some:Odd:Key")
	if !ok || source != SourceBattleNet || own != "Some:Odd:Key" {
		t.Errorf("got %q/%q/%v", source, own, ok)
	}
}

func TestNonsenseIsRefused(t *testing.T) {
	// These reach SplitID from the page. An id naming no launcher must not be
	// answered with a guess, because the answer decides which URI gets built.
	for _, id := range []string{"", "epic:", "steam:", "origin:123", ":440"} {
		if _, _, ok := SplitID(id); ok {
			t.Errorf("%q was accepted as a game id", id)
		}
	}
}

func TestLaunchURIRefusesWhatDoesNotBelongToItsLauncher(t *testing.T) {
	/*
	 * The second half of the ADR 0066 rule: the page hands over an id, and
	 * every branch validates its own alphabet before formatting a URI. A value
	 * carrying `&` or `#` into Epic's query string would rewrite what the
	 * launcher is being asked to do.
	 */
	for _, id := range []string{
		"steam:notanumber",
		"steam:440; rm -rf",
		"epic:not-hex-at-all",
		"epic:a26f991a&action=install",
		"epic:../../etc",
	} {
		if uri, err := LaunchURI(id); err == nil {
			t.Errorf("%q built the URI %q", id, uri)
		}
	}
}

func TestLaunchURIBuildsWhatEachLauncherExpects(t *testing.T) {
	if got, err := LaunchURI(SteamID("440")); err != nil || got != "steam://rungameid/440" {
		t.Errorf("steam = %q, %v", got, err)
	}
	want := "com.epicgames.launcher://apps/a26f991a5e6c4e9c9572fc200cbea47f?action=launch&silent=true"
	if got, err := LaunchURI(EpicID("a26f991a5e6c4e9c9572fc200cbea47f")); err != nil || got != want {
		t.Errorf("epic = %q, %v", got, err)
	}
}

func TestABattleNetGameHasNoLaunchURI(t *testing.T) {
	/*
	 * Deliberate, and worth pinning so nobody "fixes" it by inventing a code
	 * table. The code `battlenet://` expects is not the identifier anything on
	 * disk records — Hearthstone is `hs_beta` in product.db, `Hearthstone` in
	 * the registry and `WTCG` in a URI — and a table of guesses fails silently
	 * for every game not in it.
	 */
	if uri, err := LaunchURI(BattleNetID("Hearthstone")); err == nil {
		t.Errorf("built %q; a Blizzard game is started from its folder", uri)
	}
}

/*
 * Merging the readers.
 */

func TestOneLauncherMissingIsNotAProblemToReport(t *testing.T) {
	// Steam present, Epic absent. The commonest machine there is, and it must
	// not show anybody an error.
	got := merge(
		Result{Status: StatusOK, Games: []Game{{ID: "steam:1", Name: "A"}}},
		Result{Status: StatusNotInstalled},
		Result{Status: StatusNotInstalled},
	)
	if got.Status != StatusOK || len(got.Games) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got.Err != "" {
		t.Errorf("reported %q about a launcher that simply is not installed", got.Err)
	}
}

func TestAReaderFailingWhileAnotherWorksIsNotShown(t *testing.T) {
	/*
	 * The grid has games in it. A banner about a launcher the person may not
	 * even use is noise, and the failure is in the client log either way.
	 */
	got := merge(
		Result{Status: StatusOK, Games: []Game{{ID: "steam:1", Name: "A"}}},
		Result{Status: StatusError, Err: "epic manifests unreadable"},
	)
	if got.Status != StatusOK {
		t.Fatalf("status = %q, want ok", got.Status)
	}
	if got.Err != "" {
		t.Errorf("surfaced %q while the grid had games in it", got.Err)
	}
}

func TestAFailureWithNothingElseToShowIsReported(t *testing.T) {
	// Nothing worked, and one of them knows why. Saying so beats an empty grid
	// that looks like a broken LANcast.
	got := merge(
		Result{Status: StatusNotInstalled},
		Result{Status: StatusError, Err: "epic manifests unreadable"},
	)
	if got.Status != StatusError || got.Err != "epic manifests unreadable" {
		t.Errorf("got %+v", got)
	}
}

func TestNoLauncherAtAllIsNotInstalled(t *testing.T) {
	got := merge(Result{Status: StatusNotInstalled}, Result{Status: StatusNotInstalled})
	if got.Status != StatusNotInstalled {
		t.Errorf("status = %q", got.Status)
	}
}

func TestTheMergedListHasOneOrder(t *testing.T) {
	/*
	 * Two launchers must not make the grid's order depend on which reader ran
	 * first. Sorted by name, then by id so that two games sharing a name are
	 * still stable.
	 */
	got := merge(
		Result{Status: StatusOK, Games: []Game{{ID: "steam:2", Name: "Zeta"}, {ID: "steam:1", Name: "alpha"}}},
		Result{Status: StatusOK, Games: []Game{{ID: "epic:ff", Name: "Mid"}}},
	)
	want := []string{"alpha", "Mid", "Zeta"}
	for i, w := range want {
		if got.Games[i].Name != w {
			t.Fatalf("order = %v, want %v", []string{got.Games[0].Name, got.Games[1].Name, got.Games[2].Name}, want)
		}
	}
}
