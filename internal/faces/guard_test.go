package faces

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGuard refuses what it is told to, and records what it was asked.
type fakeGuard struct {
	refuse map[string]bool
	asked  []string
}

func (g *fakeGuard) CheckDir(dir string) error   { return g.check(dir) }
func (g *fakeGuard) CheckFile(path string) error { return g.check(path) }
func (g *fakeGuard) check(p string) error {
	g.asked = append(g.asked, filepath.Clean(p))
	if g.refuse[filepath.Clean(p)] {
		return errors.New("can be changed by BUILTIN\\Users")
	}
	return nil
}

// besideTheServer puts a worker next to the test binary, which is what
// os.Executable reports, and removes it afterwards.
func besideTheServer(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	p := filepath.Join(filepath.Dir(self), exeName())
	if _, err := os.Stat(p); err == nil {
		t.Skipf("%s already exists; not overwriting it", p)
	}
	if err := os.WriteFile(p, []byte("installed"), 0o755); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { os.Remove(p) })
	return p
}

// plantIn writes a worker into dir, the way an unprivileged account could
// in a data directory that let it add files.
func plantIn(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, exeName())
	if err := os.WriteFile(p, []byte("planted"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// R10-5 / R12-1: as the service, a worker in the faces folder is never run,
// even though it is found first by a server a person started.
func TestTheServiceRunsTheWorkerOnlyFromBesideTheServer(t *testing.T) {
	installed := besideTheServer(t)
	faces := t.TempDir()
	plantIn(t, faces)

	person := &Tool{Dir: faces, ModelsDir: faces}
	if p, _ := person.Path(); filepath.Clean(p) != filepath.Join(faces, exeName()) {
		t.Fatalf("a person's server found %q; the faces folder is still first for it", p)
	}

	svc := &Tool{Dir: faces, ModelsDir: faces, Guard: &fakeGuard{}}
	p, ok := svc.Path()
	if !ok || filepath.Clean(p) != filepath.Clean(installed) {
		t.Fatalf("the service found %q, %v; want the one beside the server, %s", p, ok, installed)
	}
}

// With nothing beside the server, the service finds nothing: not the faces
// folder, and not PATH.
func TestTheServiceDoesNotFallBackToTheFacesFolderOrPath(t *testing.T) {
	faces, onPath := t.TempDir(), t.TempDir()
	plantIn(t, faces)
	plantIn(t, onPath)
	t.Setenv("PATH", onPath)

	svc := &Tool{Dir: faces, ModelsDir: faces, Guard: &fakeGuard{}}
	if p, ok := svc.Path(); ok {
		t.Fatalf("the service found %q with no worker beside it", p)
	}
	c := svc.probe(context.Background())
	if c.Ready || c.Reason != "the face worker is not installed" {
		t.Errorf("capabilities = %+v", c)
	}
}

// The guard runs before every start, over the worker, its folder, the
// models' folder and the runtime, and any refusal stops it with a reason.
func TestTheGuardIsAskedBeforeEveryStart(t *testing.T) {
	installed := besideTheServer(t)
	faces := t.TempDir()
	runtimeDLL := filepath.Join(faces, "onnxruntime.dll")
	if err := os.WriteFile(runtimeDLL, []byte("runtime"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, refuse := range []string{filepath.Dir(installed), installed, faces, runtimeDLL} {
		t.Run(filepath.Base(refuse), func(t *testing.T) {
			g := &fakeGuard{refuse: map[string]bool{filepath.Clean(refuse): true}}
			svc := &Tool{Dir: faces, ModelsDir: faces, Runtime: runtimeDLL, Guard: g}
			if p, ok := svc.Path(); ok {
				t.Fatalf("started %q although the guard refused %s", p, refuse)
			}
			c := svc.probe(context.Background())
			if c.Ready || !strings.Contains(c.Reason, "refused") || !strings.Contains(c.Reason, "Users") {
				t.Errorf("reason = %q, want the refusal and why", c.Reason)
			}
		})
	}

	g := &fakeGuard{}
	svc := &Tool{Dir: faces, ModelsDir: faces, Runtime: runtimeDLL, Guard: g}
	for i := 0; i < 2; i++ {
		if _, ok := svc.Path(); !ok {
			t.Fatal("refused with nothing refused")
		}
	}
	if n := len(g.asked); n != 8 {
		t.Errorf("guard asked %d times over two starts (%v), want four checks each", n, g.asked)
	}
}
