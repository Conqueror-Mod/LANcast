package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * A person's shared activity, read by somebody limits apply to (ADR 0084,
 * review finding R06-1).
 *
 * Sharing publishes what the sharer watched. It does not lift what the
 * household keeps from the reader, so a title in a library hidden from the
 * reader, or above their rating ceiling, must not reach them through
 * somebody else's activity. The control in each case is the same request
 * once the limit is lifted, so a pass cannot come from the row never being
 * there.
 */

func adminIDOf(t *testing.T, a accessHarness) string {
	t.Helper()
	users, err := a.st.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Role == store.RoleAdmin {
			return u.ID
		}
	}
	t.Fatal("no admin")
	return ""
}

func activityAsMember(t *testing.T, a accessHarness, sharer string) string {
	t.Helper()
	resp := a.doAs(t, a.member, "GET", "/api/people/"+sharer+"/activity", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activity = %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSharedActivityLeavesOutTheViewersHiddenLibraries(t *testing.T) {
	a := newAccessHarness(t)
	ctx := context.Background()
	admin := adminIDOf(t, a)
	if err := a.st.SetShareActivity(ctx, admin, true); err != nil {
		t.Fatal(err)
	}
	if err := a.st.SaveProgress(ctx, a.privateFilm, admin, 0, true); err != nil {
		t.Fatal(err)
	}

	// Control: before hiding, the member sees the sharer's film.
	if body := activityAsMember(t, a, admin); !strings.Contains(body, "Home Video") {
		t.Fatalf("control: the activity does not hold the film before hiding: %.300s", body)
	}

	if code := status(t, a.hide(t, a.private)); code != http.StatusOK {
		t.Fatalf("hide = %d", code)
	}
	// The member cannot open it directly...
	if code := status(t, a.doAs(t, a.member, "GET", fmt.Sprintf("/api/items/%d", a.privateFilm), nil)); code != http.StatusNotFound {
		t.Fatalf("control: member GET item = %d", code)
	}
	// ...so they must not read it in somebody's activity either.
	if body := activityAsMember(t, a, admin); strings.Contains(body, "Home Video") {
		t.Errorf("member sees a hidden title through shared activity: %.300s", body)
	}
}

func TestSharedActivityLeavesOutWhatIsAboveTheViewersCeiling(t *testing.T) {
	a := newAccessHarness(t)
	ctx := context.Background()
	admin := adminIDOf(t, a)
	if err := a.st.SetShareActivity(ctx, admin, true); err != nil {
		t.Fatal(err)
	}
	r := "R"
	if err := a.st.UpdateItemMetadata(ctx, a.openFilm, store.ItemMetadata{ContentRating: &r}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.SaveProgress(ctx, a.openFilm, admin, 0, true); err != nil {
		t.Fatal(err)
	}
	it, err := a.st.GetItem(ctx, a.openFilm, "")
	if err != nil {
		t.Fatal(err)
	}
	if body := activityAsMember(t, a, admin); !strings.Contains(body, it.Title) {
		t.Fatalf("control: the activity does not hold %q before the ceiling: %.300s", it.Title, body)
	}
	if err := a.st.SetMaxContentRating(ctx, a.memberID(t), "PG"); err != nil {
		t.Fatal(err)
	}
	if body := activityAsMember(t, a, admin); strings.Contains(body, it.Title) {
		t.Errorf("member under a PG ceiling sees an R title through shared activity: %.300s", body)
	}
}
