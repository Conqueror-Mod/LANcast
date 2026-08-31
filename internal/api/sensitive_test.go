package api

import (
	"context"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * The sensitive endpoint (ADR 0051).
 *
 * The store tests cover what a mark means; these cover the two things the
 * handler decides — that the setting gates the gesture, and that the resolved
 * answer reaches the client, because a field the API does not send is a feature
 * that does not exist however well it works in the database.
 */

// A folder to mark. Only folders are markable (ADR 0051, amended), so the tests
// that exercise marking need one rather than a file.
func addFolder(t *testing.T, h *harness, name string) int64 {
	t.Helper()
	id, err := h.st.EnsureDerivedContainer(context.Background(), h.lib.ID,
		"gallery", h.dir+"/"+name, name, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func setSensitiveMarking(t *testing.T, h *harness, on bool) {
	t.Helper()
	resp := h.do(t, "PUT", "/api/settings", map[string]any{"sensitive_marking": on})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enabling sensitive marking: status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// With the setting off the gesture is refused rather than quietly stored. A
// mark that exists but does nothing is worse than no mark: somebody would
// believe it.
func TestMarkingIsRefusedWhileTheSettingIsOff(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.jpg", make([]byte, 16))

	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/sensitive",
		map[string]any{"sensitive": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409 while the setting is off", resp.StatusCode)
	}
}

// And with it on, the mark is stored and reported back on the item.
func TestMarkingAnItemShowsUpOnIt(t *testing.T) {
	h := newHarness(t)
	setSensitiveMarking(t, h, true)
	id := addFolder(t, h, "Folder A")

	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/sensitive",
		map[string]any{"sensitive": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	got := h.do(t, "GET", "/api/items/"+itoa(id), nil)
	var it store.Item
	decode(t, got, &it)
	if !it.Sensitive {
		t.Error("the item does not report itself sensitive")
	}
	if !it.SensitiveOwn {
		t.Error("the item does not report the mark as its own, so Unmark " +
			"would not be offered where the mark actually is")
	}
}

/*
 * Turning the setting off does not erase the marks.
 *
 * This is the half that makes the switch safe to try. A toggle that discards
 * data the second time it is pressed is a toggle nobody can experiment with,
 * and the marks are the one thing here a rescan cannot regenerate.
 */
func TestTurningTheSettingOffKeepsTheMarks(t *testing.T) {
	h := newHarness(t)
	setSensitiveMarking(t, h, true)
	id := addFolder(t, h, "Folder B")

	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/sensitive",
		map[string]any{"sensitive": true})
	resp.Body.Close()
	setSensitiveMarking(t, h, false)

	got := h.do(t, "GET", "/api/items/"+itoa(id), nil)
	var it store.Item
	decode(t, got, &it)
	if !it.SensitiveOwn {
		t.Error("turning the setting off erased the mark")
	}
}

// A body that says nothing is refused rather than read as false. "Unmark
// everything" is not a reasonable interpretation of a missing field.
func TestAnEmptyBodyIsRefused(t *testing.T) {
	h := newHarness(t)
	setSensitiveMarking(t, h, true)
	id := h.addFile(t, "a.jpg", make([]byte, 16))

	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/sensitive", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

/*
 * Only a folder can be marked (ADR 0051, amended).
 *
 * A single photograph could be marked, and it produced content that was covered
 * everywhere and viewable nowhere: the only surfaces that may lift a cover are
 * the library grid and a folder's own page, so a loose marked photo had no
 * place it could be seen. Refused on the server rather than merely hidden in
 * the menu, because the menu is not the only way to reach this endpoint.
 */
func TestOnlyAFolderCanBeMarked(t *testing.T) {
	h := newHarness(t)
	setSensitiveMarking(t, h, true)
	id := h.addFile(t, "a.jpg", make([]byte, 16))

	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/sensitive",
		map[string]any{"sensitive": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 — a single photo is not markable", resp.StatusCode)
	}
}

/*
 * And unmarking anything is still allowed.
 *
 * Photographs marked before the rule existed have to be clearable. Refusing to
 * let somebody undo a mark on the grounds that the mark should never have been
 * possible is how data becomes permanent by accident.
 */
func TestAPhotoMarkedBeforeTheRuleCanStillBeCleared(t *testing.T) {
	h := newHarness(t)
	setSensitiveMarking(t, h, true)
	id := h.addFile(t, "a.jpg", make([]byte, 16))

	// Marked directly in the store, which is how such a row got there.
	if err := h.st.SetSensitive(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/sensitive",
		map[string]any{"sensitive": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 — an existing mark must be clearable", resp.StatusCode)
	}
	resp.Body.Close()

	got := h.do(t, "GET", "/api/items/"+itoa(id), nil)
	var it store.Item
	decode(t, got, &it)
	if it.SensitiveOwn {
		t.Error("the mark survived being cleared")
	}
}
