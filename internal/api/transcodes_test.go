package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"lancast/internal/transcode"
)

// A manager needs a logger; these tests are not about what it says.
func quietTranscodeLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

/*
 * The list that did not exist when it was needed.
 *
 * A film refused to play; the app said only that it could not; and the answer —
 * three sessions holding every slot, none of which had ever delivered a byte —
 * could be had exclusively by reading lancastd.log by hand. These assert the two
 * things that would have shortened that morning: that the list is ordered so the
 * wasted slot is at the top, and that it says how much each one has served.
 */

func TestTheMostIdleConversionIsListedFirst(t *testing.T) {
	// The list is read when something has just been refused, so the row worth
	// looking at is the one nobody is using.
	views := transcodeViews([]transcode.SessionInfo{
		{ID: "watching", ItemID: 10, IdleSeconds: 2, ServedBytes: 4 << 20},
		{ID: "abandoned", ItemID: 11, IdleSeconds: 500},
		{ID: "recent", ItemID: 12, IdleSeconds: 40},
	}, nil)

	got := []string{views[0].ID, views[1].ID, views[2].ID}
	want := []string{"abandoned", "recent", "watching"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want most idle first %v", got, want)
		}
	}
}

func TestServedBytesSurvivesToThePage(t *testing.T) {
	/*
	 * The field the whole panel exists for. Zero means the slot is held for
	 * nobody, and it is what tells an abandoned session from a paused film.
	 */
	views := transcodeViews([]transcode.SessionInfo{
		{ID: "a", ItemID: 10, ServedBytes: 0},
		{ID: "b", ItemID: 11, ServedBytes: 1234},
	}, nil)

	byID := map[string]int64{}
	for _, v := range views {
		byID[v.ID] = v.ServedBytes
	}
	if byID["a"] != 0 || byID["b"] != 1234 {
		t.Errorf("served bytes = %v, want 0 and 1234 carried through", byID)
	}
}

func TestAChannelIsNotLookedUpAsAnItem(t *testing.T) {
	/*
	 * Channel ids are recorded negated. Asking the database for one would find
	 * nothing, or worse, find the item that happens to carry that number — so a
	 * live session is marked and never titled.
	 */
	asked := []int64{}
	views := transcodeViews([]transcode.SessionInfo{
		{ID: "chan", ItemID: -30598},
		{ID: "film", ItemID: 6688},
	}, func(id int64) string {
		asked = append(asked, id)
		return "Scream"
	})

	for _, v := range views {
		switch v.ID {
		case "chan":
			if !v.Live {
				t.Error("a channel was not marked live")
			}
			if v.Title != "" {
				t.Errorf("a channel was given the title %q", v.Title)
			}
		case "film":
			if v.Live {
				t.Error("a film was marked live")
			}
			if v.Title != "Scream" {
				t.Errorf("title = %q, want it resolved", v.Title)
			}
		}
	}
	if len(asked) != 1 || asked[0] != 6688 {
		t.Errorf("looked up %v, want only the real item", asked)
	}
}

func TestAMemberCannotSeeWhoIsWatchingWhat(t *testing.T) {
	/*
	 * A privacy decision rather than a permissions afterthought. A conversion
	 * names an account and a film, so this list is a list of who is watching
	 * what — which tags and watch history are careful to keep private, and a
	 * diagnostics panel must not be the way around them.
	 */
	h := newHarness(t)
	member := h.addMember(t, "viewer", "correct horse battery")

	resp := h.doAs(t, member, "GET", "/api/transcodes", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a member", resp.StatusCode)
	}

	stop := h.doAs(t, member, "DELETE", "/api/transcodes/anything", nil)
	defer stop.Body.Close()
	if stop.StatusCode != http.StatusForbidden {
		t.Errorf("stop status = %d, want 403 for a member", stop.StatusCode)
	}
}

func TestAnAdministratorIsToldTheCeiling(t *testing.T) {
	// "Two of three" is information; "two" is a number.
	h := newHarness(t)
	h.srvAPI.trans = transcode.NewManager(t.TempDir(), quietTranscodeLog())

	resp := h.do(t, "GET", "/api/transcodes", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got transcodesView
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Max != h.srvAPI.trans.MaxSessions {
		t.Errorf("max = %d, want the manager's ceiling %d", got.Max, h.srvAPI.trans.MaxSessions)
	}
	if got.Sessions == nil {
		t.Error("sessions was null; an empty list is the honest answer to nothing running")
	}
}

func TestStoppingSomethingThatHasAlreadyGoneIsNotAnError(t *testing.T) {
	/*
	 * Two administrators pressing Stop on the same row is not a failure, and
	 * the second one has got what they asked for.
	 */
	h := newHarness(t)
	h.srvAPI.trans = transcode.NewManager(t.TempDir(), quietTranscodeLog())

	resp := h.do(t, "DELETE", "/api/transcodes/never-existed", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
