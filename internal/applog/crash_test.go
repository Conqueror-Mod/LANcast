package applog

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// crashChildEnv makes the test binary, re-run as a child, crash on purpose.
const crashChildEnv = "LANCAST_APPLOG_CRASH_CHILD"

// A real crash, in a real process, with stderr thrown away as it is under the
// service manager or -H=windowsgui. Asserting that SetCrashOutput was called
// would prove nothing about whether the reason reaches the file.
func TestCaptureCrashesKeepsWhyAProcessDied(t *testing.T) {
	if dir := os.Getenv(crashChildEnv); dir != "" {
		if _, err := CaptureCrashes(dir, ClientCrashFileName); err != nil {
			t.Fatalf("child: %v", err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			panic("the film window fell over")
		}()
		<-done
		t.Fatal("child: still running after its panic")
	}

	dir := t.TempDir()
	releaseCrashOutput(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestCaptureCrashesKeepsWhyAProcessDied$")
	cmd.Env = append(os.Environ(), crashChildEnv+"="+dir)
	cmd.Stdout, cmd.Stderr = nil, nil // discarded, as under the SCM
	if err := cmd.Run(); err == nil {
		t.Fatal("the child exited cleanly; it was meant to crash")
	}

	report, err := os.ReadFile(filepath.Join(dir, ClientCrashFileName))
	if err != nil {
		t.Fatalf("no crash report: %v", err)
	}
	for _, want := range []string{"panic: the film window fell over", "goroutine "} {
		if !strings.Contains(string(report), want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}

	// The next start finds it, keeps it aside and names the cause.
	earlier, err := CaptureCrashes(dir, ClientCrashFileName)
	if err != nil {
		t.Fatal(err)
	}
	if earlier == nil {
		t.Fatal("the next start did not notice the earlier crash")
	}
	if earlier.Cause != "panic: the film window fell over" {
		t.Errorf("cause = %q", earlier.Cause)
	}
	if kept, err := os.ReadFile(earlier.Path); err != nil || !strings.Contains(string(kept), "fell over") {
		t.Errorf("the earlier report was not kept at %s: %v", earlier.Path, err)
	}
	if st, err := os.Stat(filepath.Join(dir, ClientCrashFileName)); err != nil || st.Size() != 0 {
		t.Errorf("the new run's report should start empty: %v, %v", st, err)
	}
}

// A clean start leaves nothing to report, now and at the next start: an empty
// file is not an earlier crash.
func TestCaptureCrashesIsQuietWithoutACrash(t *testing.T) {
	dir := t.TempDir()
	releaseCrashOutput(t)
	for i := 0; i < 2; i++ {
		earlier, err := CaptureCrashes(dir, ServerCrashFileName)
		if err != nil {
			t.Fatal(err)
		}
		if earlier != nil {
			t.Fatalf("start %d reported a crash that never happened: %+v", i+1, earlier)
		}
	}
}

// releaseCrashOutput lets go of this test process's crash file when the test
// ends, so its temporary directory can be removed (Windows will not delete a
// file a process holds open).
func releaseCrashOutput(t *testing.T) {
	t.Cleanup(func() { _ = debug.SetCrashOutput(nil, debug.CrashOptions{}) })
}
