//go:build windows

package singleton

import (
	"testing"

	"golang.org/x/sys/windows"
)

// Access denied can only come back for an object that exists: a name that is
// absent fails the lookup before any security check runs. Reading it as
// "absent" is what let a desktop launch start a second server beside the
// service's.
func TestClassifyOpenError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want nameState
	}{
		{"absent", windows.ERROR_FILE_NOT_FOUND, nameAbsent},
		{"absent path", windows.ERROR_PATH_NOT_FOUND, nameAbsent},
		{"exists, created by a more privileged process", windows.ERROR_ACCESS_DENIED, nameHeld},
		{"anything else is not guessed at", windows.ERROR_INVALID_HANDLE, nameUnknown},
	}
	for _, tc := range cases {
		if got := classifyOpenError(tc.err); got != tc.want {
			t.Errorf("%s: classifyOpenError(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

// The guard has to hold within a session as well, which is the case that always
// worked and must not regress.
func TestSecondAcquireIsRefused(t *testing.T) {
	const name = "LANcast-test-singleton"

	release, held, err := Acquire(name)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if !held {
		t.Fatal("first Acquire did not take the name")
	}
	defer release()

	release2, held2, err := Acquire(name)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if held2 {
		release2()
		t.Fatal("second Acquire took a name already held")
	}
}

// Release must let the name be taken again, or a restart after a clean stop
// would be refused.
func TestReleaseFreesTheName(t *testing.T) {
	const name = "LANcast-test-singleton-release"

	release, held, err := Acquire(name)
	if err != nil || !held {
		t.Fatalf("first Acquire: held=%v err=%v", held, err)
	}
	release()

	release2, held2, err := Acquire(name)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	if !held2 {
		t.Error("the name was still held after Release")
	}
	release2()
}

// A mutex this process creates in the Global namespace must be openable by a
// less privileged process, which is the behaviour the DACL exists for. Opening
// it from here only proves the descriptor parses and applies; the cross-session
// case needs a service and cannot run in a unit test.
func TestGlobalMutexIsOpenable(t *testing.T) {
	const name = `Global\LANcast-test-dacl`

	release, held, err := createMutex(name, true)
	if err != nil {
		t.Skipf("cannot create a Global object here: %v", err)
	}
	if !held {
		t.Fatal("createMutex reported the name was already held")
	}
	defer release()

	if got := globalState(name); got != nameHeld {
		t.Errorf("globalState = %v, want nameHeld — the name we just created is invisible", got)
	}
}
