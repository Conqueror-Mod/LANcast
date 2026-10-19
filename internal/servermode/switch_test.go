package servermode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"lancast/internal/store"
)

// fakeService records what a switch asked of it, in order, and fails the
// step it was told to.
type fakeService struct {
	installed bool
	automatic bool
	running   bool
	failOn    string
	calls     []string
}

func (f *fakeService) do(name string) error {
	f.calls = append(f.calls, name)
	if f.failOn == name {
		return errors.New(name + " refused")
	}
	return nil
}

func (f *fakeService) Installed() bool { return f.installed }
func (f *fakeService) Stop() error {
	if err := f.do("stop"); err != nil {
		return err
	}
	f.running = false
	return nil
}
func (f *fakeService) Start() error {
	if err := f.do("start"); err != nil {
		return err
	}
	f.running = true
	return nil
}
func (f *fakeService) SetAutomatic(on bool) error {
	name := "manual"
	if on {
		name = "automatic"
	}
	if err := f.do(name); err != nil {
		return err
	}
	f.automatic = on
	return nil
}
func (f *fakeService) Install(string) error {
	if err := f.do("install"); err != nil {
		return err
	}
	f.installed, f.automatic = true, false
	return nil
}
func (f *fakeService) Uninstall() error {
	if err := f.do("uninstall"); err != nil {
		return err
	}
	f.installed, f.running = false, false
	return nil
}

// makeLibrary builds a real library the way a server that was stopped hard
// leaves one: a database whose newest rows are only in its -wal, with no
// process holding either. It is written in a scratch folder while open and
// copied out, so the WAL is real and the handle stays behind.
func makeLibrary(t *testing.T, dir, name string, extras bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(t.TempDir(), DBName)
	s, err := store.Open(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLibrary(context.Background(), name, "movie", filepath.Join(dir, "media")); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal"} {
		write(t, filepath.Join(dir, DBName+suffix), read(t, scratch+suffix))
	}
	s.Close()
	if !extras {
		return
	}
	write(t, filepath.Join(dir, "artwork", "ab", "abcdef.jpg"), "poster")
	write(t, filepath.Join(dir, "identity", "server.key"), "identity")
	write(t, filepath.Join(dir, "secrets.key"), "secret")
	write(t, filepath.Join(dir, "config.json"), `{"from":"`+name+`"}`)
	write(t, filepath.Join(dir, "cache", "segment.ts"), "disposable")
	write(t, filepath.Join(dir, DBName+".replaced-20260101-000000"), "an old restore")
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// libraryName opens a folder's library the way a server would and says which
// one it is.
func libraryName(t *testing.T, dir string) string {
	t.Helper()
	s, err := store.Open(filepath.Join(dir, DBName))
	if err != nil {
		t.Fatalf("open the library in %s: %v", dir, err)
	}
	defer s.Close()
	libs, err := s.ListLibraries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 {
		t.Fatalf("libraries in %s = %+v, want one", dir, libs)
	}
	return libs[0].Name
}

var fixedNow = time.Date(2026, 10, 19, 18, 30, 0, 0, time.Local)

type world struct {
	env     Env
	svc     *fakeService
	answers []string
	granted []string
}

func newWorld(t *testing.T) *world {
	root := t.TempDir()
	w := &world{svc: &fakeService{}}
	w.env = Env{
		ServiceDir: filepath.Join(root, "ProgramData", "LANcast"),
		UserDir:    filepath.Join(root, "AppData", "LANcast"),
		Addr:       ":8080",
		Service:    w.svc,
		Verify:     store.VerifyCopy,
		Answers: func(addr string, _ time.Duration) bool {
			w.answers = append(w.answers, addr)
			return w.svc.running
		},
		GrantUser: func(dir string) error {
			w.granted = append(w.granted, dir)
			return os.MkdirAll(dir, 0o755)
		},
		Now: func() time.Time { return fixedNow },
	}
	return w
}

// asService puts the library where a service keeps it, with the service
// installed and running, and leaves a stale library in the person's own
// folder — the shape of a real machine that was installed without a service
// once, long ago (Aither has exactly this).
func (w *world) asService(t *testing.T) {
	makeLibrary(t, w.env.ServiceDir, "Current", true)
	w.svc.installed, w.svc.automatic, w.svc.running = true, true, true
	makeLibrary(t, w.env.UserDir, "Stale", false)
	write(t, filepath.Join(w.env.UserDir, "client", "Cookies"), "the window's profile")
}

func (w *world) asApp(t *testing.T) {
	makeLibrary(t, w.env.UserDir, "Current", true)
	write(t, filepath.Join(w.env.UserDir, "client", "Cookies"), "the window's profile")
}

func TestToAppMovesTheLibraryAndRemovesTheService(t *testing.T) {
	w := newWorld(t)
	w.asService(t)

	res, err := ToApp(w.env)
	if err != nil {
		t.Fatalf("ToApp: %v", err)
	}

	if got := libraryName(t, w.env.UserDir); got != "Current" {
		t.Errorf("the person's folder now holds %q, want the service's library", got)
	}
	if HasLibrary(w.env.ServiceDir) {
		t.Error("the service's folder still holds lancast.db, so the app would keep pinning to it")
	}
	// The order is the safety argument (ADR 0092 §6).
	if want := []string{"stop", "manual", "uninstall"}; !reflect.DeepEqual(w.svc.calls, want) {
		t.Errorf("service calls = %v, want %v", w.svc.calls, want)
	}
	if !reflect.DeepEqual(w.granted, []string{w.env.UserDir}) {
		t.Errorf("granted = %v, want the person's folder once, before the copy", w.granted)
	}

	// Kept, not deleted: the old database and the stale one are both still
	// on disk under names nothing opens.
	if res.MovedAside == "" || !exists(res.MovedAside) {
		t.Errorf("moved aside = %q, which is not on disk", res.MovedAside)
	}
	if !strings.HasPrefix(filepath.Base(res.MovedAside), DBName+".moved-") {
		t.Errorf("the source was set aside as %q", res.MovedAside)
	}
	if res.Superseded == "" || !exists(res.Superseded) {
		t.Errorf("superseded = %q, which is not on disk", res.Superseded)
	}
	// And the stale database's log went with it rather than staying to be
	// replayed into the new one.
	if !exists(res.Superseded + "-wal") {
		t.Error("the stale database's -wal was not set aside with it")
	}

	// What travels and what stays.
	for _, rel := range []string{"artwork/ab/abcdef.jpg", "identity/server.key", "secrets.key"} {
		if !exists(filepath.Join(w.env.UserDir, filepath.FromSlash(rel))) {
			t.Errorf("%s did not travel", rel)
		}
	}
	if got := read(t, filepath.Join(w.env.UserDir, "config.json")); !strings.Contains(got, "Current") {
		t.Errorf("config.json = %s, want the service's settings", got)
	}
	if exists(filepath.Join(w.env.UserDir, "cache", "segment.ts")) {
		t.Error("the transcode cache travelled")
	}
	if exists(filepath.Join(w.env.UserDir, DBName+".replaced-20260101-000000")) {
		t.Error("an old set-aside database travelled")
	}
	if got := read(t, filepath.Join(w.env.UserDir, "client", "Cookies")); got != "the window's profile" {
		t.Errorf("the window's profile was touched: %q", got)
	}
	for _, name := range []string{incomingName, incomingName + "-wal", incomingName + "-shm"} {
		if exists(filepath.Join(w.env.UserDir, name)) {
			t.Errorf("%s was left behind", name)
		}
	}
	// The source's other files stay where they were, for the person to remove.
	if !exists(filepath.Join(w.env.ServiceDir, "artwork", "ab", "abcdef.jpg")) {
		t.Error("the source's artwork was removed; nothing may be deleted")
	}
}

func TestToAppPutsTheServiceBackWhenTheCopyDoesNotOpen(t *testing.T) {
	w := newWorld(t)
	w.asService(t)
	w.env.Verify = func(string) error { return errors.New("database disk image is malformed") }

	if _, err := ToApp(w.env); err == nil {
		t.Fatal("ToApp succeeded with a copy that did not open")
	}

	if got := libraryName(t, w.env.ServiceDir); got != "Current" {
		t.Errorf("the service's library is now %q", got)
	}
	// Untouched: the stale library keeps its name, because nothing was placed.
	if got := libraryName(t, w.env.UserDir); got != "Stale" {
		t.Errorf("the person's folder now holds %q, want it untouched", got)
	}
	if !w.svc.running || !w.svc.automatic || !w.svc.installed {
		t.Errorf("service = %+v, want it running and starting at boot again", w.svc)
	}
	if exists(filepath.Join(w.env.UserDir, incomingName)) {
		t.Error("the copy that failed was left behind")
	}
}

func TestToAppPutsTheServiceBackWhenItCannotBeSetToManual(t *testing.T) {
	w := newWorld(t)
	w.asService(t)
	w.svc.failOn = "manual"

	if _, err := ToApp(w.env); err == nil {
		t.Fatal("ToApp succeeded without stopping the service starting at boot")
	}
	if got := libraryName(t, w.env.ServiceDir); got != "Current" {
		t.Errorf("the service's library is now %q", got)
	}
	if got := libraryName(t, w.env.UserDir); got != "Stale" {
		t.Errorf("the person's folder now holds %q; nothing should have been placed", got)
	}
	if !w.svc.running {
		t.Error("the service was not started again")
	}
}

func TestToAppChangesNothingWhenTheServiceWillNotStop(t *testing.T) {
	w := newWorld(t)
	w.asService(t)
	w.svc.failOn = "stop"

	if _, err := ToApp(w.env); err == nil {
		t.Fatal("ToApp went ahead with the service still holding the database")
	}
	if len(w.granted) != 0 {
		t.Error("the person's folder was changed before the service had stopped")
	}
	if !HasLibrary(w.env.ServiceDir) || libraryName(t, w.env.UserDir) != "Stale" {
		t.Error("a folder changed")
	}
}

func TestToAppKeepsTheLibraryWhenOnlyTheUninstallFails(t *testing.T) {
	w := newWorld(t)
	w.asService(t)
	w.svc.failOn = "uninstall"

	res, err := ToApp(w.env)
	if err != nil {
		t.Fatalf("ToApp: %v — the library was across; this is a warning, not a failure", err)
	}
	if res.Warning == "" {
		t.Error("no warning about the service that is still registered")
	}
	if libraryName(t, w.env.UserDir) != "Current" {
		t.Error("the library did not move")
	}
	if w.svc.automatic || w.svc.running {
		t.Errorf("service = %+v, want it stopped and not starting at boot", w.svc)
	}

	// Switching again is how it is finished.
	w.svc.failOn, w.svc.calls = "", nil
	if _, err := ToApp(w.env); err != nil {
		t.Fatalf("finishing: %v", err)
	}
	if !reflect.DeepEqual(w.svc.calls, []string{"uninstall"}) || w.svc.installed {
		t.Errorf("finishing called %v and left installed=%v", w.svc.calls, w.svc.installed)
	}
	if libraryName(t, w.env.UserDir) != "Current" {
		t.Error("finishing touched the library")
	}
}

func TestToServiceMovesTheLibraryAndStartsTheService(t *testing.T) {
	w := newWorld(t)
	w.asApp(t)
	w.env.GrantUser = nil

	res, err := ToService(w.env)
	if err != nil {
		t.Fatalf("ToService: %v", err)
	}
	if got := libraryName(t, w.env.ServiceDir); got != "Current" {
		t.Errorf("the service's folder holds %q", got)
	}
	if HasLibrary(w.env.UserDir) {
		t.Error("the person's folder still holds lancast.db")
	}
	if want := []string{"install", "start", "automatic"}; !reflect.DeepEqual(w.svc.calls, want) {
		t.Errorf("service calls = %v, want %v", w.svc.calls, want)
	}
	if !reflect.DeepEqual(w.answers, []string{":8080"}) {
		t.Errorf("asked for an answer at %v, want the app's address", w.answers)
	}
	if !exists(res.MovedAside) {
		t.Errorf("the old copy %q is not on disk", res.MovedAside)
	}
	if exists(filepath.Join(w.env.ServiceDir, "client")) {
		t.Error("the window's profile travelled into the service's folder")
	}
	if !exists(filepath.Join(w.env.UserDir, "client", "Cookies")) {
		t.Error("the window's profile was moved")
	}
}

func TestToServiceUndoesAServiceThatDoesNotAnswer(t *testing.T) {
	w := newWorld(t)
	w.asApp(t)
	w.env.Answers = func(string, time.Duration) bool { return false }

	if _, err := ToService(w.env); err == nil {
		t.Fatal("ToService succeeded with a service that never answered")
	}
	if w.svc.installed {
		t.Error("the service that did not answer is still installed")
	}
	if HasLibrary(w.env.ServiceDir) {
		t.Error("the copy kept its name, so the app would pin itself to a folder it cannot write")
	}
	if got := libraryName(t, w.env.UserDir); got != "Current" {
		t.Errorf("the person's library is now %q, want it untouched", got)
	}
	// The copy is kept as failed, not deleted.
	matches, _ := filepath.Glob(filepath.Join(w.env.ServiceDir, DBName+".failed-*"))
	if len(matches) != 1 {
		t.Errorf("failed copies = %v, want one", matches)
	}
}

func TestToServiceChangesNothingWhenItCannotInstall(t *testing.T) {
	w := newWorld(t)
	w.asApp(t)
	w.svc.failOn = "install"

	if _, err := ToService(w.env); err == nil {
		t.Fatal("ToService succeeded without a service")
	}
	if HasLibrary(w.env.ServiceDir) || exists(filepath.Join(w.env.ServiceDir, incomingName)) {
		t.Error("the service's folder was left holding a copy")
	}
	if libraryName(t, w.env.UserDir) != "Current" {
		t.Error("the person's library changed")
	}
}

func TestToServiceRefusesWhenAlreadyAService(t *testing.T) {
	w := newWorld(t)
	w.asService(t)
	if _, err := ToService(w.env); err == nil {
		t.Fatal("ToService ran over a running service")
	}
	if len(w.svc.calls) != 0 {
		t.Errorf("service calls = %v, want none", w.svc.calls)
	}
}

func TestASwitchRefusesWhenThereIsNoRoom(t *testing.T) {
	w := newWorld(t)
	w.asApp(t)
	w.env.FreeBytes = func(string) (uint64, bool) { return 1 << 20, true }

	_, err := ToService(w.env)
	if err == nil || !strings.Contains(err.Error(), "free") {
		t.Fatalf("ToService = %v, want a refusal about free space", err)
	}
	if exists(w.env.ServiceDir) {
		t.Error("the destination was created before the space was checked")
	}
	if len(w.svc.calls) != 0 {
		t.Errorf("service calls = %v, want none", w.svc.calls)
	}
}

// An interrupted copy leaves only incoming files. They are not a library, and
// the next switch clears them and starts again.
func TestAnInterruptedCopyIsNotALibraryAndIsRedone(t *testing.T) {
	w := newWorld(t)
	w.asApp(t)
	write(t, filepath.Join(w.env.ServiceDir, incomingName), "half a database")
	write(t, filepath.Join(w.env.ServiceDir, incomingName+"-wal"), "half a log")

	if HasLibrary(w.env.ServiceDir) {
		t.Fatal("a folder holding only an incoming copy counts as a library")
	}
	if _, err := ToService(w.env); err != nil {
		t.Fatalf("ToService after an interrupted copy: %v", err)
	}
	if got := libraryName(t, w.env.ServiceDir); got != "Current" {
		t.Errorf("the service's folder holds %q", got)
	}
}

func TestSkip(t *testing.T) {
	for rel, want := range map[string]bool{
		"lancast.db":                         false,
		"lancast.db-wal":                     false,
		"lancast.db-shm":                     true,
		"lancast.db.incoming":                true,
		"lancast.db.moved-20261019-183000":   true,
		"lancast.db.replaced-20261019-18300": true,
		"cache":                              true,
		"cache/segment.ts":                   true,
		"client/Cookies":                     true,
		"artwork/ab/abc.jpg":                 false,
		"artwork/cache/x":                    false, // only the top level is judged
		"identity/server.key":                false,
		"secrets.key":                        false,
		"tls/cert.pem":                       false,
		"config.json":                        false,
	} {
		if got := skip(rel); got != want {
			t.Errorf("skip(%q) = %v, want %v", rel, got, want)
		}
	}
}
