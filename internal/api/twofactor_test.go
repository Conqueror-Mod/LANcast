package api

import (
	"encoding/base32"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"lancast/internal/auth"
	"lancast/internal/secret"
)

const tfPassword = "a long enough password"

func (h *harness) withSealer(t *testing.T) {
	t.Helper()
	sl, err := secret.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.srvAPI.SetTwoFactorSealer(sl)
}

func decodeInto(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

// turnOnTwoFactor enrols the harness's signed-in account and returns the
// secret (standing in for the phone) and the recovery codes.
func turnOnTwoFactor(t *testing.T, h *harness) ([]byte, []string) {
	t.Helper()
	resp := h.authed(t, "POST", "/api/profile/two-factor/setup", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d", resp.StatusCode)
	}
	var setup struct{ Key, URL string }
	decodeInto(t, resp, &setup)
	if !strings.HasPrefix(setup.URL, "otpauth://totp/LANcast:") {
		t.Fatalf("url = %s", setup.URL)
	}
	sec, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(setup.Key, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	// The previous step's code, so the current one is still unspent for the
	// sign-in the test does next.
	resp = h.authed(t, "POST", "/api/profile/two-factor", map[string]any{
		"code": auth.TOTPCode(sec, time.Now().Add(-30*time.Second)),
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable: %d", resp.StatusCode)
	}
	var on struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	decodeInto(t, resp, &on)
	if len(on.RecoveryCodes) != 10 {
		t.Fatalf("%d recovery codes", len(on.RecoveryCodes))
	}
	return sec, on.RecoveryCodes
}

type loginAnswer struct {
	Authenticated bool   `json:"authenticated"`
	TwoFactor     bool   `json:"two_factor"`
	Challenge     string `json:"challenge"`
}

func passwordStep(t *testing.T, h *harness, user, pw string) (loginAnswer, *http.Response) {
	t.Helper()
	resp := h.do(t, "POST", "/api/auth/login", map[string]any{"username": user, "password": pw})
	var a loginAnswer
	decodeInto(t, resp, &a)
	return a, resp
}

func codeStep(t *testing.T, h *harness, body map[string]any) *http.Response {
	t.Helper()
	resp := h.do(t, "POST", "/api/auth/login/two-factor", body)
	resp.Body.Close()
	return resp
}

func gotSession(resp *http.Response) bool {
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return true
		}
	}
	return false
}

func TestThePasswordAloneNoLongerSignsIn(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	sec, _ := turnOnTwoFactor(t, h)

	a, resp := passwordStep(t, h, testUser, tfPassword)
	if a.Authenticated || !a.TwoFactor || a.Challenge == "" || gotSession(resp) {
		t.Fatalf("password step: %+v, cookie=%v", a, gotSession(resp))
	}
	code := auth.TOTPCode(sec, time.Now())
	resp = codeStep(t, h, map[string]any{"challenge": a.Challenge, "code": code})
	if resp.StatusCode != http.StatusOK || !gotSession(resp) {
		t.Fatalf("code step: %d cookie=%v", resp.StatusCode, gotSession(resp))
	}

	// The challenge is spent, and so is the code: a fresh challenge with the
	// same code (read over a shoulder, say) is refused.
	if resp := codeStep(t, h, map[string]any{"challenge": a.Challenge, "code": code}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("spent challenge: %d", resp.StatusCode)
	}
	a2, _ := passwordStep(t, h, testUser, tfPassword)
	if resp := codeStep(t, h, map[string]any{"challenge": a2.Challenge, "code": code}); resp.StatusCode != http.StatusUnauthorized || gotSession(resp) {
		t.Fatalf("reused code: %d", resp.StatusCode)
	}
}

func TestAWrongPasswordNeverReachesTheCodeStep(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	turnOnTwoFactor(t, h)
	resp := h.do(t, "POST", "/api/auth/login", map[string]any{"username": testUser, "password": "wrong"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestARecoveryCodeSignsInOnce(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	_, codes := turnOnTwoFactor(t, h)

	a, _ := passwordStep(t, h, testUser, tfPassword)
	// Typed in capitals without the dash, as somebody reading it off paper might.
	typed := strings.ToUpper(strings.ReplaceAll(codes[3], "-", ""))
	if resp := codeStep(t, h, map[string]any{"challenge": a.Challenge, "recovery_code": typed}); resp.StatusCode != http.StatusOK || !gotSession(resp) {
		t.Fatalf("recovery code: %d", resp.StatusCode)
	}
	a, _ = passwordStep(t, h, testUser, tfPassword)
	if resp := codeStep(t, h, map[string]any{"challenge": a.Challenge, "recovery_code": codes[3]}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("recovery code reused: %d", resp.StatusCode)
	}

	var st struct {
		Enabled           bool `json:"enabled"`
		RecoveryCodesLeft int  `json:"recovery_codes_left"`
	}
	decodeInto(t, h.authed(t, "GET", "/api/profile/two-factor", nil), &st)
	if !st.Enabled || st.RecoveryCodesLeft != 9 {
		t.Fatalf("state %+v", st)
	}
}

func TestAChallengeAllowsFiveTries(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	sec, _ := turnOnTwoFactor(t, h)
	a, _ := passwordStep(t, h, testUser, tfPassword)
	for i := 0; i < twoFactorChallengeTries; i++ {
		codeStep(t, h, map[string]any{"challenge": a.Challenge, "code": "000000"})
	}
	if resp := codeStep(t, h, map[string]any{"challenge": a.Challenge, "code": auth.TOTPCode(sec, time.Now())}); resp.StatusCode != http.StatusUnauthorized || gotSession(resp) {
		t.Fatalf("sixth try on one challenge: %d", resp.StatusCode)
	}
}

// Restored onto another machine, the sealed secret cannot be opened. Codes
// stop working; recovery codes are the way in.
func TestOnAnotherMachineOnlyRecoveryCodesWork(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	sec, codes := turnOnTwoFactor(t, h)
	h.withSealer(t) // a different key: the other machine

	a, _ := passwordStep(t, h, testUser, tfPassword)
	if resp := codeStep(t, h, map[string]any{"challenge": a.Challenge, "code": auth.TOTPCode(sec, time.Now())}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("code with the wrong key: %d", resp.StatusCode)
	}
	if resp := codeStep(t, h, map[string]any{"challenge": a.Challenge, "recovery_code": codes[0]}); resp.StatusCode != http.StatusOK {
		t.Fatalf("recovery code: %d", resp.StatusCode)
	}
}

func TestTurningItOffNeedsThePassword(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	turnOnTwoFactor(t, h)

	resp := h.authed(t, "DELETE", "/api/profile/two-factor", map[string]any{"current_password": "wrong"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	resp = h.authed(t, "POST", "/api/profile/two-factor/recovery-codes", map[string]any{"current_password": "wrong"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("new codes, wrong password: %d", resp.StatusCode)
	}
	resp = h.authed(t, "DELETE", "/api/profile/two-factor", map[string]any{"current_password": tfPassword})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("right password: %d", resp.StatusCode)
	}
	a, resp := passwordStep(t, h, testUser, tfPassword)
	if !a.Authenticated || !gotSession(resp) {
		t.Fatalf("after turning it off: %+v", a)
	}
}

func TestNewRecoveryCodesReplaceTheOld(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	_, old := turnOnTwoFactor(t, h)
	var fresh struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	decodeInto(t, h.authed(t, "POST", "/api/profile/two-factor/recovery-codes", map[string]any{"current_password": tfPassword}), &fresh)
	if len(fresh.RecoveryCodes) != 10 {
		t.Fatalf("%d codes", len(fresh.RecoveryCodes))
	}
	a, _ := passwordStep(t, h, testUser, tfPassword)
	if resp := codeStep(t, h, map[string]any{"challenge": a.Challenge, "recovery_code": old[0]}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an old code still works: %d", resp.StatusCode)
	}
	if resp := codeStep(t, h, map[string]any{"challenge": a.Challenge, "recovery_code": fresh.RecoveryCodes[0]}); resp.StatusCode != http.StatusOK {
		t.Fatalf("a new code: %d", resp.StatusCode)
	}
}

// An administrator can take it off for somebody who lost their phone, and
// sees who has it; nothing lets them put it on for somebody else.
func TestAnAdminCanOnlyTakeItOff(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	m := h.member(t, "sam", tfPassword)

	saved := h.cookie
	h.cookie = m.cookie
	turnOnTwoFactor(t, h)
	h.cookie = saved

	var list struct {
		Users []map[string]any `json:"users"`
	}
	decodeInto(t, h.authed(t, "GET", "/api/users", nil), &list)
	seen := false
	for _, u := range list.Users {
		if u["id"] == m.id {
			seen = u["two_factor"] == true
		} else if _, has := u["two_factor"]; has {
			t.Fatalf("two_factor on an account without it: %v", u)
		}
	}
	if !seen {
		t.Fatalf("listing does not show sam's two-factor: %v", list.Users)
	}

	// A member cannot take it off anybody.
	resp := h.asUser(t, m, "DELETE", "/api/users/"+m.id+"/two-factor", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member removing: %d", resp.StatusCode)
	}

	resp = h.authed(t, "DELETE", "/api/users/"+m.id+"/two-factor", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin removing: %d", resp.StatusCode)
	}
	a, _ := passwordStep(t, h, "sam", tfPassword)
	if !a.Authenticated {
		t.Fatalf("sam still asked for a code: %+v", a)
	}
}

func TestWithoutASealingKeyItIsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	var st struct {
		Available bool `json:"available"`
	}
	decodeInto(t, h.authed(t, "GET", "/api/profile/two-factor", nil), &st)
	if st.Available {
		t.Fatal("available without a sealer")
	}
	resp := h.authed(t, "POST", "/api/profile/two-factor/setup", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("setup: %d", resp.StatusCode)
	}
}

func TestEnablingNeedsACodeFromTheApp(t *testing.T) {
	h := newHarness(t)
	h.secure(t, tfPassword)
	h.withSealer(t)
	resp := h.authed(t, "POST", "/api/profile/two-factor", map[string]any{"code": "123456"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("enable without setup: %d", resp.StatusCode)
	}
	h.authed(t, "POST", "/api/profile/two-factor/setup", nil).Body.Close()
	resp = h.authed(t, "POST", "/api/profile/two-factor", map[string]any{"code": "000000"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong code: %d", resp.StatusCode)
	}
	a, _ := passwordStep(t, h, testUser, tfPassword)
	if !a.Authenticated {
		t.Fatal("a failed enrolment turned two-factor on")
	}
}
