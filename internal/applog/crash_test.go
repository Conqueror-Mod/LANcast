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
	// The header, and where: the test's own goroutine, in LANcast's code.
	if !strings.HasPrefix(earlier.Cause, "panic: the film window fell over (in lancast/internal/applog.") ||
		!strings.Contains(earlier.Cause, " at crash_test.go:") {
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

// The first crash this ever caught had no header line at all, only stacks, and
// its cause came out empty. testdata/callback-limit-crash.txt is the top of
// that report, as it reached the file on 2026-11-01.
func TestCrashCauseNamesTheSiteWhenTheHeaderIsMissing(t *testing.T) {
	got := crashCause(filepath.Join("testdata", "callback-limit-crash.txt"))
	want := "crashed in syscall.compileCallback, called from lancast/internal/clientwindow.Monitors at placement_windows.go:50"
	if got != want {
		t.Errorf("cause =\n  %q\nwant\n  %q", got, want)
	}
}

func TestCauseOf(t *testing.T) {
	for _, c := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"nothing", []string{"", "some text"}, ""},
		{"header only", []string{"fatal error: out of memory"}, "fatal error: out of memory"},
		{
			"panic in LANcast's own code",
			[]string{
				"panic: boom", "",
				"goroutine 7 [running]:",
				"main.play(0x1)",
				"\tlancast/cmd/lancast/player.go:12 +0x1f",
				"created by main.start in goroutine 1",
				"\tlancast/cmd/lancast/player.go:3 +0x2",
			},
			"panic: boom (in main.play at player.go:12)",
		},
		{
			// Only the crashing goroutine counts; LANcast frames in another
			// goroutine are not where it happened.
			"no LANcast frame in the crashing goroutine",
			[]string{
				"goroutine 3 [running]:",
				"runtime.throw({0x1, 0x2})",
				"\truntime/panic.go:1 +0x1",
				"net/http.(*conn).serve(0x1)",
				"\tnet/http/server.go:2 +0x1",
				"",
				"goroutine 1 [select]:",
				"main.main()",
				"\tlancast/cmd/lancast/main.go:9 +0x1",
			},
			"crashed in net/http.(*conn).serve",
		},
	} {
		if got := causeOf(c.lines); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFrameFileKeepsSpacesInAPath(t *testing.T) {
	for in, want := range map[string]string{
		"\tlancast/internal/clientwindow/placement_windows.go:50 +0x6e fp=0x1 sp=0x2": "placement_windows.go:50",
		"\tD:/My Projects/LANcast/internal/applog/crash_test.go:28 +0x45":             "crash_test.go:28",
		"\tsyscall/syscall_windows.go:235":                                            "syscall_windows.go:235",
		"\tno file here":                                                              "",
	} {
		if got := frameFile(in); got != want {
			t.Errorf("frameFile(%q) = %q, want %q", in, got, want)
		}
	}
}
