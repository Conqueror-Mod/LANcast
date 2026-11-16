package omdb

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

// R08-6: OMDb takes its key only in the query, so it must not reach the error.
func TestNetworkErrorDoesNotCarryTheKey(t *testing.T) {
	c := New("SECRET-OMDB-KEY",
		WithBaseURL("https://omdb.example.invalid"),
		WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	c.MaxRetries = 0

	_, err := c.Ratings(context.Background(), "tt0133093")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET-OMDB-KEY") || strings.Contains(err.Error(), "apikey") {
		t.Errorf("the key reached the error: %q", err)
	}
}
