//go:build windows

package webview2

import (
	"encoding/json"
	"errors"
	"testing"
)

/*
 * A binding that panics rejects the page's promise instead of ending the
 * desktop app (review R12-3). Bindings run on the window's UI thread, and a
 * malformed game icon made one panic every time the Games screen drew it.
 */
func TestAPanickingBindingIsAnErrorNotACrash(t *testing.T) {
	w := &webview{bindings: map[string]interface{}{
		"boom": func(s string) (string, error) {
			var b []byte
			_ = b[len(s)] // index out of range, as the icon decoder did
			return "unreachable", nil
		},
		"fine": func(s string) (string, error) { return "got " + s, nil },
	}}
	arg, _ := json.Marshal("icon")
	res, err := w.callbinding(rpcMessage{ID: 1, Method: "boom", Params: []json.RawMessage{arg}})
	if !errors.Is(err, errBindingPanicked) {
		t.Fatalf("want errBindingPanicked, got %v (result %v)", err, res)
	}
	if res != nil {
		t.Fatalf("a panicked binding returned %v", res)
	}

	// The next call works: the panic left nothing locked behind it.
	res, err = w.callbinding(rpcMessage{ID: 2, Method: "fine", Params: []json.RawMessage{arg}})
	if err != nil || res != "got icon" {
		t.Fatalf("after a panic: %v, %v", res, err)
	}
}
