package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"testing"

	"lancast/internal/store"
)

func picturePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{180, 40, 40, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (h *harness) uploadPicture(t *testing.T, cookie *http.Cookie, body []byte) (int, string) {
	t.Helper()
	resp := h.raw(t, "PUT", "/api/profile/picture", cookie, "", body)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return resp.StatusCode, ""
	}
	var got struct {
		Picture string `json:"picture"`
	}
	decode(t, resp, &got)
	return resp.StatusCode, got.Picture
}

func (h *harness) mePicture(t *testing.T, cookie *http.Cookie) any {
	t.Helper()
	var me struct {
		User map[string]any `json:"user"`
	}
	decode(t, h.raw(t, "GET", "/api/auth/status", cookie, "", nil), &me)
	return me.User["picture"]
}

// Upload, see it on /api/auth/status, fetch both sizes as JPEG, clear it.
func TestAPictureRoundTrips(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")

	if got := h.mePicture(t, h.cookie); got != nil {
		t.Fatalf("picture before any upload = %v, want null", got)
	}
	status, hash := h.uploadPicture(t, h.cookie, picturePNG(t))
	if status != http.StatusOK || len(hash) != 64 {
		t.Fatalf("upload = %d %q, want 200 and a hash", status, hash)
	}
	if got := h.mePicture(t, h.cookie); got != hash {
		t.Errorf("status reports picture %v, want %q", got, hash)
	}

	for size, want := range map[string]int{"": 256, "256": 256, "64": 64} {
		resp := h.raw(t, "GET", "/api/pictures/"+hash+"?size="+size, h.cookie, "", nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("size %q: status %d", size, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("size %q: content type %q", size, ct)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "private, max-age=31536000, immutable" {
			t.Errorf("size %q: cache-control %q", size, cc)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || cfg.Width != want {
			t.Errorf("size %q: %d px, %v; want %d", size, cfg.Width, err, want)
		}
	}

	resp := h.raw(t, "DELETE", "/api/profile/picture", h.cookie, "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	if got := h.mePicture(t, h.cookie); got != nil {
		t.Errorf("picture after clearing = %v, want null", got)
	}
	resp = h.raw(t, "GET", "/api/pictures/"+hash, h.cookie, "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a cleared picture is still served: %d", resp.StatusCode)
	}
}

func TestAnUploadThatIsNotAPictureIsRefused(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	if status, _ := h.uploadPicture(t, h.cookie, []byte("<svg/>")); status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

func TestPicturesNeedASignIn(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	_, hash := h.uploadPicture(t, h.cookie, picturePNG(t))
	resp := h.raw(t, "GET", "/api/pictures/"+hash, nil, "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status without a session = %d, want 401", resp.StatusCode)
	}
}

// The setting stops members and not admins (ADR 0121 §5).
func TestMemberPicturesOffRefusesMembersOnly(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	m := h.member(t, "Gerda", "another long password")

	cur := h.settings.Get()
	cur.MemberPictures = false
	if err := h.settings.Set(cur); err != nil {
		t.Fatal(err)
	}
	if status, _ := h.uploadPicture(t, m.cookie, picturePNG(t)); status != http.StatusForbidden {
		t.Errorf("member upload with the setting off = %d, want 403", status)
	}
	if status, _ := h.uploadPicture(t, h.cookie, picturePNG(t)); status != http.StatusOK {
		t.Errorf("admin upload with the setting off = %d, want 200", status)
	}
}

// Other people see it on /api/people; an admin sees it on /api/users and can
// clear it. A member cannot clear somebody else's.
func TestOthersSeeAPictureAndAnAdminCanClearIt(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	m := h.member(t, "Gerda", "another long password")
	_, hash := h.uploadPicture(t, m.cookie, picturePNG(t))

	var people struct {
		People []store.Person `json:"people"`
	}
	decode(t, h.authed(t, "GET", "/api/people", nil), &people)
	if len(people.People) != 1 || people.People[0].Picture == nil || *people.People[0].Picture != hash {
		t.Errorf("people = %+v, want Gerda with picture %q", people.People, hash)
	}

	var users struct {
		Users []map[string]any `json:"users"`
	}
	decode(t, h.authed(t, "GET", "/api/users", nil), &users)
	found := false
	for _, u := range users.Users {
		if u["id"] == m.id {
			found = u["picture"] == hash
		}
	}
	if !found {
		t.Errorf("users = %v, want Gerda's picture %q", users.Users, hash)
	}

	other := h.member(t, "Otto", "a third long password")
	resp := h.asUser(t, other, "DELETE", "/api/users/"+m.id+"/picture", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a member clearing somebody else's picture = %d, want 403", resp.StatusCode)
	}

	resp = h.authed(t, "DELETE", "/api/users/"+m.id+"/picture", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin clear = %d, want 204", resp.StatusCode)
	}
	if got := h.mePicture(t, m.cookie); got != nil {
		t.Errorf("Gerda's picture after an admin cleared it = %v, want null", got)
	}
}

// Named, though the gate's route walk already covers it: a guest from a paired
// server is not shown anybody's face (ADR 0121 §4).
func TestAGuestCannotFetchAPicture(t *testing.T) {
	f := newRedeemFixture(t)
	_, hash := f.h.uploadPicture(t, f.h.cookie, picturePNG(t))
	token := guestToken(t, f)
	resp := f.asGuest(t, token, http.MethodGet, "/api/pictures/"+hash)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}
