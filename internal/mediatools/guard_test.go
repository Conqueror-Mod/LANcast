package mediatools

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// root is an absolute root on this platform, so the rules' IsAbs check and
// their walk up to the root behave as they do for real.
func root() string {
	if runtime.GOOS == "windows" {
		return `C:\`
	}
	return "/"
}

func at(parts ...string) string { return filepath.Join(append([]string{root()}, parts...)...) }

// fakeReader answers from a map; a missing path does not exist.
type fakeReader map[string]Descriptor

func (f fakeReader) Read(p string) (Descriptor, error) { return f[filepath.Clean(p)], nil }

const sidUsers = "S-1-5-32-545"
const sidAuthUsers = "S-1-5-11"
const sidChris = "S-1-5-21-1-2-3-1001"

// admin is a folder as Program Files has it: SYSTEM, Administrators and
// TrustedInstaller may change it, Users may read and run.
func admin() Descriptor {
	return Descriptor{Exists: true, Owner: sidAdministrators, ACEs: []ACE{
		{SID: sidSystem, Mask: rightGenericAll, Allow: true},
		{SID: sidAdministrators, Mask: rightGenericAll, Allow: true},
		{SID: sidTrustedInstaller, Mask: rightGenericAll, Allow: true},
		{SID: sidUsers, Name: `BUILTIN\Users`, Mask: 0x1200a9, Allow: true},
	}}
}

func with(d Descriptor, aces ...ACE) Descriptor {
	d.ACEs = append(append([]ACE(nil), d.ACEs...), aces...)
	return d
}

// protectedTree is C:\ProgramData\LANcast\tools as Protect leaves it, under a
// LANcast folder that, like ProgramData's children, lets Users add files.
func protectedTree() fakeReader {
	tools := at("ProgramData", "LANcast", "tools")
	return fakeReader{
		root():                          with(admin(), ACE{SID: sidAuthUsers, Mask: rightAppendData, Allow: true}),
		at("ProgramData"):               admin(),
		at("ProgramData", "LANcast"):    with(admin(), ACE{SID: sidUsers, Name: `BUILTIN\Users`, Mask: rightWriteData | rightAppendData, Allow: true}),
		tools:                           admin(),
		filepath.Join(tools, "ffprobe"): admin(),
		filepath.Join(tools, "ffmpeg"):  admin(),
	}
}

func TestACLGuardAcceptsAProtectedFolder(t *testing.T) {
	g := NewACLGuard(protectedTree())
	tools := at("ProgramData", "LANcast", "tools")
	if err := g.CheckDir(tools); err != nil {
		t.Fatalf("a folder only Administrators and SYSTEM can change was refused: %v", err)
	}
	if err := g.CheckFile(filepath.Join(tools, "ffprobe")); err != nil {
		t.Fatalf("a protected binary was refused: %v", err)
	}
}

func TestACLGuardRefusals(t *testing.T) {
	tools := at("ProgramData", "LANcast", "tools")
	probe := filepath.Join(tools, "ffprobe")
	cases := []struct {
		name   string
		change func(f fakeReader)
		file   bool
		want   string
	}{
		{"the folder is owned by a user", func(f fakeReader) {
			d := f[tools]
			d.Owner, d.OwnerName = sidChris, `AITHER\Chris`
			f[tools] = d
		}, false, `owned by AITHER\Chris`},
		{"Users may add files to the folder", func(f fakeReader) {
			f[tools] = with(f[tools], ACE{SID: sidUsers, Name: `BUILTIN\Users`, Mask: rightWriteData, Allow: true})
		}, false, `changed by BUILTIN\Users`},
		{"a user has full control of the folder", func(f fakeReader) {
			f[tools] = with(f[tools], ACE{SID: sidChris, Mask: rightGenericAll, Allow: true})
		}, false, sidChris},
		{"the folder has no DACL", func(f fakeReader) {
			d := f[tools]
			d.NullDACL, d.ACEs = true, nil
			f[tools] = d
		}, false, "no access list"},
		{"the folder is a junction", func(f fakeReader) {
			d := f[tools]
			d.Link = true
			f[tools] = d
		}, false, "is a link"},
		{"an entry LANcast cannot decode", func(f fakeReader) {
			f[tools] = with(f[tools], ACE{SID: sidUsers, Allow: true, Unknown: true})
		}, false, "cannot read"},
		{"Users may rename children of the folder above", func(f fakeReader) {
			p := at("ProgramData", "LANcast")
			f[p] = with(f[p], ACE{SID: sidUsers, Mask: rightDeleteChild, Allow: true})
		}, false, "the folder above it"},
		{"the folder above belongs to a user", func(f fakeReader) {
			p := at("ProgramData", "LANcast")
			d := f[p]
			d.Owner = sidChris
			f[p] = d
		}, false, "the folder above it"},
		{"the binary was planted by a user before the folder was closed", func(f fakeReader) {
			d := f[probe]
			d.Owner = sidChris
			f[probe] = d
		}, true, "ffprobe is owned by"},
		{"Users may overwrite the binary", func(f fakeReader) {
			f[probe] = with(f[probe], ACE{SID: sidUsers, Mask: rightWriteData, Allow: true})
		}, true, "ffprobe can be changed"},
		{"the binary is a link", func(f fakeReader) {
			d := f[probe]
			d.Link = true
			f[probe] = d
		}, true, "is a link"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := protectedTree()
			c.change(f)
			g := NewACLGuard(f)
			var err error
			if c.file {
				err = g.CheckFile(probe)
			} else {
				err = g.CheckDir(tools)
			}
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("reason %q does not say %q", err, c.want)
			}
		})
	}
}

// Entries that cannot make the folder writable must not refuse it: rules that
// refused everything would push people to switch the check off.
func TestACLGuardIgnoresEntriesThatGrantNothingHere(t *testing.T) {
	tools := at("ProgramData", "LANcast", "tools")
	f := protectedTree()
	f[tools] = with(f[tools],
		// CREATOR OWNER (OI)(CI)(IO)(F): applies to what is created inside.
		ACE{SID: "S-1-3-0", Mask: rightGenericAll, Allow: true, InheritOnly: true},
		// A deny entry only takes rights away.
		ACE{SID: sidUsers, Mask: rightGenericAll, Allow: false},
		// Read, execute and the attribute rights cannot put a binary there.
		ACE{SID: sidUsers, Mask: 0x1200a9 | 0x100 | 0x10, Allow: true},
	)
	if err := NewACLGuard(f).CheckDir(tools); err != nil {
		t.Fatalf("refused: %v", err)
	}
}

func TestACLGuardRefusesWhatIsNotThere(t *testing.T) {
	g := NewACLGuard(fakeReader{})
	if err := g.CheckDir(at("nowhere")); err == nil {
		t.Error("a folder that does not exist was accepted")
	}
	if err := g.CheckDir("relative"); err == nil {
		t.Error("a relative path was accepted")
	}
}

// fakeStat is the Unix side's map, with links as a second map.
type fakeStat struct {
	modes map[string]FileMode
	links map[string]string
}

func (f fakeStat) Stat(p string) (FileMode, error) { return f.modes[filepath.Clean(p)], nil }
func (f fakeStat) Resolve(p string) (string, error) {
	if to, ok := f.links[filepath.Clean(p)]; ok {
		return to, nil
	}
	return filepath.Clean(p), nil
}

func rootDir() FileMode { return FileMode{Exists: true, UID: 0, Mode: fs.ModeDir | 0o755} }

func unixTree() fakeStat {
	return fakeStat{modes: map[string]FileMode{
		at():                         rootDir(),
		at("usr"):                    rootDir(),
		at("usr", "bin"):             rootDir(),
		at("usr", "bin", "ffprobe"):  {Exists: true, Mode: 0o755},
		at("usr", "bin", "ffmpeg"):   {Exists: true, Mode: 0o755},
		at("tmp"):                    {Exists: true, Mode: fs.ModeDir | fs.ModeSticky | 0o777},
		at("tmp", "x"):               {Exists: true, UID: 1000, Mode: fs.ModeDir | 0o755},
		at("srv"):                    {Exists: true, UID: 0, Mode: fs.ModeDir | 0o777},
		at("srv", "ff"):              rootDir(),
		at("home", "lancast", "bin"): {Exists: true, UID: 990, Mode: fs.ModeDir | 0o755},
		at("home", "lancast"):        {Exists: true, UID: 990, Mode: fs.ModeDir | 0o750},
		at("home"):                   rootDir(),
		at("snap", "bin"):            rootDir(),
		at("snap"):                   rootDir(),
	}, links: map[string]string{
		at("snap", "bin", "ffprobe"): at("usr", "bin", "ffprobe"),
		at("snap", "bin", "evil"):    at("tmp", "x", "ffprobe"),
	}}
}

func TestModeGuard(t *testing.T) {
	g := NewModeGuard(unixTree(), 990)
	for _, ok := range []string{at("usr", "bin"), at("home", "lancast", "bin"), at("snap", "bin")} {
		if err := g.CheckDir(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	if err := g.CheckFile(at("usr", "bin", "ffprobe")); err != nil {
		t.Errorf("a root-owned binary was refused: %v", err)
	}
	if err := g.CheckFile(at("snap", "bin", "ffprobe")); err != nil {
		t.Errorf("a link into /usr/bin was refused: %v", err)
	}
	refused := map[string]string{
		at("tmp", "x"):  "owned by uid 1000",               // another account's folder
		at("srv", "ff"): "can be written by every account", // a world-writable parent without the sticky bit
	}
	for dir, want := range refused {
		err := g.CheckDir(dir)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CheckDir(%s) = %v, want a refusal saying %q", dir, err, want)
		}
	}
	if err := g.CheckFile(at("snap", "bin", "evil")); err == nil {
		t.Error("a link out to another account's folder was accepted")
	}
	// Another account's folder is fine for the account it belongs to.
	if err := NewModeGuard(unixTree(), 1000).CheckDir(at("tmp", "x")); err != nil {
		t.Errorf("own folder under a sticky /tmp refused: %v", err)
	}
}

// fakeGuard refuses the folders it is given, by key.
type fakeGuard map[string]string

func (f fakeGuard) CheckDir(dir string) error {
	if why, ok := f[pathKey(dir)]; ok {
		return errors.New(why)
	}
	return nil
}
func (f fakeGuard) CheckFile(string) error { return nil }

// forServiceWith is ForService with a protect step that only creates the
// folder; the real one needs an administrator, and guard_windows_test.go
// covers it where it can.
func forServiceWith(g Guard, dataDir, found string) (string, error) {
	return forService(&Locator{
		Guard:   g,
		protect: func(d string) error { return os.MkdirAll(d, 0o755) },
		owner:   func(string) error { return nil },
	}, dataDir, found)
}

func keepPath(t *testing.T) {
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
}

// The route R09-1 was found on: the recorded folder is the user's, and the
// service must neither run it nor stay silent about why.
func TestLocatorRefusesTheConfiguredFolderAndSaysWhy(t *testing.T) {
	keepPath(t)
	user := fakeToolDir(t)
	managed := fakeToolDir(t)
	l := &Locator{Guard: fakeGuard{pathKey(user): "owned by the user"}, Managed: managed}
	r := l.Resolve(user)
	if !r.OK || r.Dir != managed {
		t.Fatalf("Resolve = %+v, want the managed folder", r)
	}
	if len(r.Refused) != 1 || r.Refused[0].Dir != user || r.Refused[0].Reason != "owned by the user" {
		t.Errorf("refusals = %+v, want the user's folder with its reason", r.Refused)
	}
	if got := l.Last(); got.Dir != managed || len(got.Refused) != 1 {
		t.Errorf("Last() = %+v, want what Resolve answered", got)
	}
}

// A refused folder still on PATH would be run anyway, because every caller
// looks ffmpeg up with exec.LookPath.
func TestLocatorTakesRefusedFoldersOffPath(t *testing.T) {
	keepPath(t)
	bad := fakeToolDir(t)
	open := t.TempDir() // no tools, but a folder the guard would not trust
	fine := t.TempDir()
	sep := string(os.PathListSeparator)
	os.Setenv("PATH", bad+sep+open+sep+fine)

	l := &Locator{Guard: fakeGuard{pathKey(bad): "writable", pathKey(open): "writable"}}
	r := l.Resolve("")
	if r.OK {
		t.Fatalf("Resolve = %+v, want nothing found", r)
	}
	if len(r.Refused) != 1 || r.Refused[0].Dir != bad {
		t.Errorf("refusals = %+v, want the PATH folder holding ffprobe", r.Refused)
	}
	if got := os.Getenv("PATH"); got != fine {
		t.Errorf("PATH = %q, want only %q left", got, fine)
	}
}

// Without a guard nothing changes for a server a person started: the folder
// is used and PATH keeps every entry.
func TestLocatorWithoutAGuardAcceptsAnything(t *testing.T) {
	keepPath(t)
	dir := fakeToolDir(t)
	other := t.TempDir()
	os.Setenv("PATH", other)
	r := (&Locator{}).Resolve(dir)
	if !r.OK || r.Dir != dir || len(r.Refused) != 0 {
		t.Fatalf("Resolve = %+v", r)
	}
	if got := os.Getenv("PATH"); got != dir+string(os.PathListSeparator)+other {
		t.Errorf("PATH = %q", got)
	}
}

// The copy the installer makes of a per-user ffmpeg: both binaries, ffprobe
// in place, and the source left alone.
func TestForServiceCopiesAFolderTheGuardRefuses(t *testing.T) {
	keepPath(t)
	src := t.TempDir()
	for _, n := range []string{"ffmpeg", "ffprobe"} {
		if err := os.WriteFile(filepath.Join(src, exeName(n)), []byte(n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data := t.TempDir()
	got, err := forServiceWith(fakeGuard{pathKey(src): "the user's"}, data, src)
	if err != nil {
		t.Fatal(err)
	}
	if got != ManagedDir(data) {
		t.Fatalf("recorded %q, want the managed folder", got)
	}
	for _, n := range []string{"ffmpeg", "ffprobe"} {
		b, err := os.ReadFile(filepath.Join(got, exeName(n)))
		if err != nil || string(b) != n {
			t.Errorf("%s not copied: %q, %v", n, b, err)
		}
	}

	// A folder that passes is recorded as it is, with nothing copied.
	again, err := forServiceWith(fakeGuard{}, t.TempDir(), src)
	if err != nil || again != src {
		t.Errorf("a passing folder: got %q, %v; want it recorded unchanged", again, err)
	}
}
