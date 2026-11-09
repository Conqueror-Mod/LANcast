package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestPictureSetReplaceClear(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, "u_a", "Ada", "x", RoleMember); err != nil {
		t.Fatal(err)
	}

	first, err := s.SetPicture(ctx, "u_a", []byte("large-1"), []byte("small-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SetPicture(ctx, "u_a", []byte("large-2"), []byte("small-2"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("a new picture kept the old hash; a client would keep showing the old one")
	}
	if got, _ := s.PictureHash(ctx, "u_a"); got != second {
		t.Errorf("hash = %q, want the replacement %q", got, second)
	}
	if _, err := s.PictureByHash(ctx, first, PictureLarge); !errors.Is(err, ErrNotFound) {
		t.Errorf("old hash still served: err = %v", err)
	}
	small, err := s.PictureByHash(ctx, second, PictureSmall)
	if err != nil || !bytes.Equal(small, []byte("small-2")) {
		t.Errorf("small = %q, %v; want small-2", small, err)
	}

	if had, err := s.ClearPicture(ctx, "u_a"); err != nil || !had {
		t.Fatalf("clear = %v, %v; want true", had, err)
	}
	if got, _ := s.PictureHash(ctx, "u_a"); got != "" {
		t.Errorf("hash after clear = %q, want none", got)
	}
}

func TestDeletingAnAccountDeletesItsPicture(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, "u_a", "Ada", "x", RoleMember); err != nil {
		t.Fatal(err)
	}
	hash, err := s.SetPicture(ctx, "u_a", []byte("l"), []byte("s"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, "u_a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PictureByHash(ctx, hash, PictureLarge); !errors.Is(err, ErrNotFound) {
		t.Errorf("a deleted account's picture is still served: err = %v", err)
	}
}

// A backup is the database and nothing else (ADR 0058), which is why pictures
// live in it (ADR 0121). This is the property that decision rests on.
func TestABackupCarriesThePicture(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	if _, err := src.CreateUser(ctx, "u_a", "Ada", "x", RoleMember); err != nil {
		t.Fatal(err)
	}
	hash, err := src.SetPicture(ctx, "u_a", []byte("large"), []byte("small"))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "lancast-backup.db")
	if _, err := src.Snapshot(ctx, dst); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := restored.PictureByHash(ctx, hash, PictureLarge)
	if err != nil || !bytes.Equal(got, []byte("large")) {
		t.Errorf("restored picture = %q, %v; want the original", got, err)
	}
}
