package meta

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestRedactURLErrorDropsQuery(t *testing.T) {
	in := &url.Error{
		Op:  "Get",
		URL: "https://api.themoviedb.org/3/movie/603?api_key=SECRETKEY&language=en-US",
		Err: context.DeadlineExceeded,
	}
	got := RedactURLError(in)
	if strings.Contains(got.Error(), "SECRETKEY") || strings.Contains(got.Error(), "api_key") {
		t.Errorf("key survived: %q", got)
	}
	if !strings.Contains(got.Error(), "api.themoviedb.org/3/movie/603") {
		t.Errorf("lost which request failed: %q", got)
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Error("the cause no longer unwraps")
	}
	plain := errors.New("tmdb: status 500")
	if RedactURLError(plain) != plain {
		t.Error("a non-URL error was changed")
	}
}
