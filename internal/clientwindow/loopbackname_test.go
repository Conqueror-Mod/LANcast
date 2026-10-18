package clientwindow

import "testing"

func TestTheLoopbackNameIsAPlainHostName(t *testing.T) {
	arg, err := loopbackArg("media.example.net")
	if err != nil || arg != "--host-resolver-rules=MAP media.example.net 127.0.0.1" {
		t.Fatalf("arg = %q, %v", arg, err)
	}
	// Anything that could add a switch, or map more than one name, is refused.
	for _, bad := range []string{"", "a b.net", "a,b.net", "media.example.net --x", `media"net`,
		"-media.net", "media..net", ".media.net", "*.media.net", "media.net:8080"} {
		if _, err := loopbackArg(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
