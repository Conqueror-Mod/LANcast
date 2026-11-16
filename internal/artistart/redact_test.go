package artistart

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

// R08-6: TheAudioDB's key is a path segment, so it must not reach the error.
func TestNetworkErrorDoesNotCarryTheKey(t *testing.T) {
	c := New("SECRET-ADB-KEY")
	c.http = &http.Client{Transport: failingTransport{}}
	_, err := c.Lookup(context.Background(), "Some Band")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET-ADB-KEY") {
		t.Errorf("the key reached the error: %q", err)
	}
}
