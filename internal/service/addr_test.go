package service

import "testing"

// The port lives in the service's command line (ADR 0089); changing it must
// change -addr and leave every other token exactly as it was.
func TestWithAddrRewritesOnlyTheAddress(t *testing.T) {
	cases := []struct{ in, addr, want string }{
		{`"C:\Program Files\LANcast\LANcast.exe" service run -data C:\ProgramData\LANcast -addr :8080`, ":9090",
			`"C:\Program Files\LANcast\LANcast.exe" service run -data C:\ProgramData\LANcast -addr :9090`},
		{`"C:\Program Files\LANcast\LANcast.exe" service run -data "D:\My Data" -addr=127.0.0.1:8080`, "127.0.0.1:9090",
			`"C:\Program Files\LANcast\LANcast.exe" service run -data "D:\My Data" -addr=127.0.0.1:9090`},
		{`C:\LANcast\LANcast.exe service run -data C:\ProgramData\LANcast`, ":9090",
			`C:\LANcast\LANcast.exe service run -data C:\ProgramData\LANcast -addr :9090`},
	}
	for _, c := range cases {
		if got := withAddr(c.in, c.addr); got != c.want {
			t.Errorf("withAddr(%s)\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

// What is written reads back as the same arguments, which is what the service
// control manager and ConfiguredAddr will both do with it.
func TestARewrittenCommandLineReadsBack(t *testing.T) {
	in := `"C:\Program Files\LANcast\LANcast.exe" service run -data "D:\My Data" -addr :8080`
	data, addr := dataDirFromArgs(splitCommandLine(withAddr(in, "[::]:9443")))
	if data != `D:\My Data` || addr != "[::]:9443" {
		t.Errorf("read back data=%q addr=%q", data, addr)
	}
}
