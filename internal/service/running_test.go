package service

import (
	"strings"
	"testing"
)

// The real thing, copied from an installed machine: a quoted program path with
// a space in it, followed by the run mode and the pinned flags. Splitting this
// on spaces alone yields "C:\Program" as the executable and never finds -data.
const installedBinPath = `"C:\Program Files\LANcast\LANcast-Server.exe" service run -data C:\ProgramData\LANcast -addr :8080`

func TestSplitCommandLineKeepsAQuotedPathWhole(t *testing.T) {
	args := splitCommandLine(installedBinPath)
	want := []string{
		`C:\Program Files\LANcast\LANcast-Server.exe`,
		"service", "run",
		"-data", `C:\ProgramData\LANcast`,
		"-addr", ":8080",
	}
	if len(args) != len(want) {
		t.Fatalf("split into %d args, want %d: %q", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestDataDirFromInstalledServiceCommandLine(t *testing.T) {
	dataDir, addr := dataDirFromArgs(splitCommandLine(installedBinPath))
	if dataDir != `C:\ProgramData\LANcast` {
		t.Errorf("data dir = %q, want the ProgramData path", dataDir)
	}
	if addr != ":8080" {
		t.Errorf("addr = %q, want :8080", addr)
	}
}

// A hand-edited service entry may join flag and value with =. Getting this
// wrong puts a false directory in front of an operator who is already confused,
// which is worse than showing none.
func TestDataDirAcceptsEqualsForm(t *testing.T) {
	dataDir, addr := dataDirFromArgs([]string{
		"service", "run", `-data=D:\LANcast Data`, "-addr=127.0.0.1:9000",
	})
	if dataDir != `D:\LANcast Data` {
		t.Errorf("data dir = %q, want the D: path", dataDir)
	}
	if addr != "127.0.0.1:9000" {
		t.Errorf("addr = %q, want 127.0.0.1:9000", addr)
	}
}

// A flag with nothing after it must not run off the end of the slice.
func TestDataDirToleratesATrailingFlag(t *testing.T) {
	dataDir, _ := dataDirFromArgs([]string{"service", "run", "-data"})
	if dataDir != "" {
		t.Errorf("data dir = %q, want empty for a flag with no value", dataDir)
	}
}

// What the operator actually reads. The service case has to name the service,
// the pid, and the data directory — the three things that took a CIM query to
// find when this message said only "already running".
func TestDescribeNamesTheService(t *testing.T) {
	r := Running{Service: true, PID: 21160, DataDir: `C:\ProgramData\LANcast`, Addr: ":8080"}
	got := r.Describe()
	for _, want := range []string{"lancastd", "21160", `C:\ProgramData\LANcast`, ":8080"} {
		if !strings.Contains(got, want) {
			t.Errorf("describe() = %q, missing %q", got, want)
		}
	}
	if !strings.Contains(r.Hint(), "administrator") {
		t.Errorf("a service hint must mention administrator rights, got %q", r.Hint())
	}
}

// Nothing established: the caller falls back to its generic message, so this
// must be empty rather than a sentence with holes in it.
func TestDescribeIsEmptyWhenNothingIsKnown(t *testing.T) {
	if got := (Running{}).Describe(); got != "" {
		t.Errorf("describe() = %q, want empty", got)
	}
	// The hint still has something useful to say without a known holder.
	if (Running{}).Hint() == "" {
		t.Error("hint is empty even in the unknown case")
	}
}
