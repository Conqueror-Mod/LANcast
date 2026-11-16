package api

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// R07-9: a report that cannot reach the webhook logs why, and not the
// webhook's URL, whose path is its token.
func TestAFailedReportDoesNotLogTheWebhook(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	// Port 1 on loopback refuses at once: the failure "check the connection"
	// is written for.
	hook := "http://127.0.0.1:1/api/webhooks/123456/SECRET-WEBHOOK-TOKEN"
	h.srvAPI.reportURL = func() string { return hook }
	var logged bytes.Buffer
	h.srvAPI.log = slog.New(slog.NewTextHandler(&logged, nil))

	wantError(t, h.authed(t, "POST", "/api/report", map[string]any{"what": "x"}), 502, "not_sent")

	if !strings.Contains(logged.String(), "problem report not sent") {
		t.Fatalf("the failure was not logged:\n%s", logged.String())
	}
	if strings.Contains(logged.String(), "SECRET-WEBHOOK-TOKEN") {
		t.Errorf("the webhook token reached the log:\n%s", logged.String())
	}
}
