package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

/*
 * Changing the port at the boundary (ADR 0089). Rewriting the service and
 * the restart that rolls back are Windows service work, proven as the
 * installed service; here it is who may ask, which ports are refused before
 * anything is touched, and that a refused request changes nothing.
 */

type fakePortChanger struct{ asked []string }

func (f *fakePortChanger) Change(port string) error {
	f.asked = append(f.asked, port)
	return nil
}

func portHarness(t *testing.T) (*harness, *fakePortChanger) {
	t.Helper()
	h := newHarness(t)
	h.secure(t, "a long enough password")
	h.srvAPI.listenAddr = "127.0.0.1:8080"
	f := &fakePortChanger{}
	h.srvAPI.SetPortChanger(f)
	return h, f
}

func errMessage(t *testing.T, resp *http.Response) string {
	t.Helper()
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	decode(t, resp, &e)
	return e.Error.Message
}

func TestThePortIsChangedThroughTheService(t *testing.T) {
	h, f := portHarness(t)
	free := freePort(t)

	resp := h.authed(t, "PUT", "/api/network/port", map[string]any{"port": free})
	var got struct {
		Port       string `json:"port"`
		Restarting bool   `json:"restarting"`
	}
	decode(t, resp, &got)
	if resp.StatusCode != http.StatusAccepted || got.Port != strconv.Itoa(free) || !got.Restarting {
		t.Fatalf("%d %+v", resp.StatusCode, got)
	}
	if len(f.asked) != 1 || f.asked[0] != strconv.Itoa(free) {
		t.Errorf("the service was asked for %v", f.asked)
	}
}

func TestPortsThatAreRefusedBeforeAnythingChanges(t *testing.T) {
	h, f := portHarness(t)
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	busy := held.Addr().(*net.TCPAddr).Port

	cases := []struct {
		name string
		port int
		code int
		want string
	}{
		{"privileged", 80, http.StatusBadRequest, "1024 to 65535"},
		{"too high", 70000, http.StatusBadRequest, "1024 to 65535"},
		{"the public certificate's", 443, http.StatusBadRequest, "public certificate"},
		{"DLNA's", 8200, http.StatusBadRequest, "DLNA"},
		{"the current one", 8080, http.StatusBadRequest, "already listens"},
		{"one something else holds", busy, http.StatusConflict, "in use"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := h.authed(t, "PUT", "/api/network/port", map[string]any{"port": c.port})
			if msg := errMessage(t, resp); resp.StatusCode != c.code || !strings.Contains(msg, c.want) {
				t.Errorf("%d %q, want %d mentioning %q", resp.StatusCode, msg, c.code, c.want)
			}
		})
	}
	if len(f.asked) != 0 {
		t.Errorf("a refused port reached the service: %v", f.asked)
	}
}

// Not the installed service: the port is the -addr it was started with, and
// the pane says so instead of offering a change that could not happen.
func TestOnlyTheInstalledServiceCanChangeItsPort(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	h.srvAPI.listenAddr = "127.0.0.1:8080"

	if code := status(t, h.authed(t, "PUT", "/api/network/port", map[string]any{"port": freePort(t)})); code != http.StatusConflict {
		t.Errorf("PUT = %d, want 409", code)
	}
	var net networkStatus
	decode(t, h.authed(t, "GET", "/api/network", nil), &net)
	if net.PortChangeable || !strings.Contains(net.PortFixedBecause, "-addr") {
		t.Errorf("network = changeable %v, because %q", net.PortChangeable, net.PortFixedBecause)
	}
}

func TestOnlyAnAdministratorChangesThePort(t *testing.T) {
	a := newAccessHarness(t)
	f := &fakePortChanger{}
	a.srvAPI.SetPortChanger(f)
	if code := status(t, a.doAs(t, a.member, "PUT", "/api/network/port", map[string]any{"port": freePort(t)})); code != http.StatusForbidden {
		t.Errorf("member PUT = %d, want 403", code)
	}
	if len(f.asked) != 0 {
		t.Errorf("a member's request reached the service: %v", f.asked)
	}
}

// The outcome is written by the restart helper, after the process that was
// asked has gone; the pane reads it back for a day.
func TestTheLastPortChangeIsReported(t *testing.T) {
	h, _ := portHarness(t)
	if err := WritePortChange(h.srvAPI.dataDir, PortChange{From: "8080", To: "9090",
		Error: "the server did not answer on port 9090, so it went back to port 8080", At: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	var net networkStatus
	decode(t, h.authed(t, "GET", "/api/network", nil), &net)
	if !net.PortChangeable || net.LastPortChange == nil || net.LastPortChange.OK || net.LastPortChange.To != "9090" {
		t.Errorf("network = %+v", net)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
