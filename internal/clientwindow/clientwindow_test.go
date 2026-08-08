package clientwindow

import "testing"

// Check must never panic and must be safe to call before anything is created —
// the launcher calls it to decide between a window and a browser, so a panic
// here would take out the app on exactly the machines least able to run it.
func TestCheckIsSafeToCall(t *testing.T) {
	if err := Check(); err != nil {
		t.Logf("no window available here: %v", err)
	}
}

// Available agrees with Check. Two ways to ask the same question drift apart
// eventually unless one is defined in terms of the other.
func TestAvailableAgreesWithCheck(t *testing.T) {
	if got, want := Available(), Check() == nil; got != want {
		t.Errorf("Available() = %v, Check() == nil is %v", got, want)
	}
}
