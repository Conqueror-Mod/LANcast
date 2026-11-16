package meta

import (
	"errors"
	"fmt"
	"net/url"
)

/*
 * RedactURLError keeps a provider's API key out of an error's text (R08-6).
 *
 * http.Client.Do fails with a *url.Error, and its Error() is the whole request
 * URL followed by the cause: `Get "https://api.themoviedb.org/3/movie/603?
 * api_key=<key>&…": context deadline exceeded`. url.Error redacts a password
 * in the userinfo and nothing else, so a key in the query reached lancastd.log
 * on every timeout, and Report an issue can attach that log. With the built-in
 * keys (ADR 0110) the key that travelled was the project's own.
 *
 * The replacement names the method, scheme, host and path, which is what a
 * person reading the log needs to know which request failed, and wraps the
 * cause with %w so errors.Is(err, context.DeadlineExceeded) still answers.
 * Any other error is returned unchanged.
 */
func RedactURLError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	where := "request"
	if u, perr := url.Parse(ue.URL); perr == nil && u.Host != "" {
		where = u.Scheme + "://" + u.Host + u.EscapedPath()
	}
	return fmt.Errorf("%s %s: %w", ue.Op, where, ue.Err)
}
