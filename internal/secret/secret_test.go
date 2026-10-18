package secret

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSealedValuesOpenAndDoNotRepeat(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Seal([]byte("api-key"))
	b, _ := s.Seal([]byte("api-key"))
	if bytes.Equal(a, b) {
		t.Error("the same value sealed twice came out the same: the nonce is not fresh")
	}
	if bytes.Contains(a, []byte("api-key")) {
		t.Error("the sealed value contains the secret")
	}
	if got, err := s.Open(a); err != nil || string(got) != "api-key" {
		t.Errorf("open = %q, %v", got, err)
	}
}

// The key is kept and reused; a value sealed before a restart opens after it.
func TestTheKeyFileIsKept(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Load(dir)
	sealed, _ := s1.Seal([]byte("x"))
	s2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s2.Open(sealed); err != nil || string(got) != "x" {
		t.Errorf("after reloading the key: %q, %v", got, err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, KeyFile)); fi == nil || fi.Size() != keySize {
		t.Error("no key file of the right size")
	}
}

// A database restored onto another machine: its values are sealed under a
// key this one does not have, and say so rather than returning garbage.
func TestAnotherKeyCannotOpen(t *testing.T) {
	s1, _ := Load(t.TempDir())
	s2, _ := Load(t.TempDir())
	sealed, _ := s1.Seal([]byte("x"))
	if _, err := s2.Open(sealed); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("err = %v, want ErrCannotOpen", err)
	}
	if _, err := s1.Open([]byte("short")); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("a damaged value: %v", err)
	}
}

// A key file of the wrong size is left alone, not replaced.
func TestABadKeyFileIsNotReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, KeyFile)
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("a wrong-sized key was accepted")
	}
	if b, _ := os.ReadFile(path); string(b) != "short" {
		t.Error("the key file was overwritten")
	}
}
