package report

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection reset")
}

// R07-9: a report that cannot reach the webhook must not put the webhook's
// path, which is its id and token, into the error the handler logs.
func TestSendErrorDoesNotCarryTheWebhookToken(t *testing.T) {
	hook := "https://discord.example.invalid/api/webhooks/123456/SECRET-WEBHOOK-TOKEN"
	err := Send(context.Background(), &http.Client{Transport: failingTransport{}}, hook, Report{What: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET-WEBHOOK-TOKEN") || strings.Contains(err.Error(), "123456") {
		t.Errorf("the webhook reached the error: %q", err)
	}
	if !strings.Contains(err.Error(), "discord.example.invalid") || !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("lost where and why: %q", err)
	}
}
