//go:build windows

package serverapp

import (
	"reflect"
	"testing"
)

// The handover carries this launch's choices to the client: hidden for the
// `tray` entry points (an icon was asked for), shown for a double-click, and
// a non-default address or data directory, which the client would otherwise
// guess differently.
func TestTheHandoverCarriesTheLaunchsChoices(t *testing.T) {
	cases := []struct {
		addr, data string
		hidden     bool
		want       []string
	}{
		{":8080", "", true, []string{"-hidden"}},
		{":8080", "", false, nil},
		{":9090", `C:\ProgramData\LANcast`, true, []string{"-hidden", "-addr", ":9090", "-data", `C:\ProgramData\LANcast`}},
		{"", "", false, nil},
	}
	for _, c := range cases {
		if got := clientArgs(c.addr, c.data, c.hidden); !reflect.DeepEqual(got, c.want) {
			t.Errorf("clientArgs(%q, %q, %v) = %q, want %q", c.addr, c.data, c.hidden, got, c.want)
		}
	}
}
