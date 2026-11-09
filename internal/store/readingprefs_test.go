package store

import (
	"context"
	"errors"
	"testing"
)

/*
 * Reading and listening defaults per account (settings-3-plan step 4,
 * revision 93): nil until chosen, and changed one field at a time.
 */

func f64p(f float64) *float64 { return &f }

func TestReadingPrefsStartUnchosen(t *testing.T) {
	st, me, _ := pinFixture(t)
	p, err := st.ReadingPrefsFor(context.Background(), me)
	if err != nil {
		t.Fatal(err)
	}
	if p.BookSpeed != nil || p.BookSleep != nil || p.ComicLayout != nil || p.ComicFit != nil {
		t.Errorf("a new account has chosen nothing, got %+v", p)
	}
}

func TestReadingPrefsChangeOneFieldAtATime(t *testing.T) {
	st, me, _ := pinFixture(t)
	ctx := context.Background()
	if err := st.SetReadingPrefs(ctx, me, ReadingPrefs{BookSpeed: f64p(1.5), ComicLayout: strp("spread")}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReadingPrefs(ctx, me, ReadingPrefs{BookSleep: strp("30")}); err != nil {
		t.Fatal(err)
	}
	p, err := st.ReadingPrefsFor(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if p.BookSpeed == nil || *p.BookSpeed != 1.5 || p.ComicLayout == nil || *p.ComicLayout != "spread" {
		t.Errorf("setting the sleep timer must leave the rest alone, got %+v", p)
	}
	if p.BookSleep == nil || *p.BookSleep != "30" {
		t.Errorf("sleep = %v, want 30", p.BookSleep)
	}
	if p.ComicFit != nil {
		t.Errorf("fit was never set, got %v", *p.ComicFit)
	}
}

func TestReadingPrefsForNobody(t *testing.T) {
	st, _, _ := pinFixture(t)
	if _, err := st.ReadingPrefsFor(context.Background(), "u_nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("read for a missing account: %v, want ErrNotFound", err)
	}
	if err := st.SetReadingPrefs(context.Background(), "u_nobody", ReadingPrefs{ComicFit: strp("width")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("write for a missing account: %v, want ErrNotFound", err)
	}
}
