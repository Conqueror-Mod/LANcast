package main

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"lancast/internal/clientwindow"
)

/*
 * R11-2. The bindings that decide which servers this computer trusts are
 * injected into every document the window shows, a remote server's page
 * included. They answer only a caller holding the token the picker page
 * carries, and these tests hold that in place.
 */

// trustBindings are the ones a server's page must not be able to drive.
var trustBindings = []string{
	"lancastServers", "lancastServerProbe", "lancastServerAccept",
	"lancastServerAcceptRotation", "lancastServerConnect",
	"lancastServerConnectLocal", "lancastServerForget",
}

func TestTrustBindingsRefuseACallerWithoutThePickersToken(t *testing.T) {
	l := &launcher{}
	b := l.serverBindings(func() clientwindow.Controller { return nil })
	for _, name := range trustBindings {
		fn := reflect.ValueOf(b[name])
		if !fn.IsValid() {
			t.Errorf("%s is not bound", name)
			continue
		}
		ft := fn.Type()
		if ft.NumIn() == 0 || ft.In(0).Kind() != reflect.String {
			t.Errorf("%s does not take the token first", name)
			continue
		}
		// Every argument a wrong token and junk: the guard must answer before
		// anything is parsed, fetched or saved.
		args := make([]reflect.Value, ft.NumIn())
		args[0] = reflect.ValueOf("not-the-token")
		for i := 1; i < len(args); i++ {
			args[i] = reflect.ValueOf("127.0.0.1:1")
		}
		out := fn.Call(args)[0].Interface().(map[string]any)
		if out["error"] != "only the server picker can do this" {
			t.Errorf("%s answered a caller without the token: %v", name, out)
		}
	}
}

// The page passes the token on every call to them, and the token is
// substituted -- a placeholder left in would lock the picker out of itself.
func TestThePickerPageCarriesAndUsesTheToken(t *testing.T) {
	page := pickerPage("")
	if strings.Contains(page, "__PICKER_TOKEN__") {
		t.Fatal("the token placeholder was not substituted")
	}
	if !strings.Contains(page, `const PICKER = "`+pickerToken+`"`) {
		t.Fatal("the page does not carry this process's token")
	}
	for _, name := range trustBindings {
		calls := regexp.MustCompile(`window\.`+name+`\(([^)]*)`).FindAllStringSubmatch(page, -1)
		for _, c := range calls {
			if !strings.HasPrefix(strings.TrimSpace(c[1]), "PICKER") {
				t.Errorf("a call to %s does not pass the token: %q", name, c[0])
			}
		}
	}
	if !fromPicker(pickerToken) || fromPicker("") || fromPicker(pickerToken+"x") {
		t.Error("fromPicker does not accept exactly the token")
	}
}
