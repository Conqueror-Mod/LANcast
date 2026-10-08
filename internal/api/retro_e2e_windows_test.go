//go:build windows

package api

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/retro/host"
	"lancast/internal/retro/libretro"
	"lancast/internal/retro/remote"
	"lancast/internal/store"
)

/*
 * The whole of stage 2's protocol, end to end, with nothing faked but the
 * window: this server with a retro library on disk, a ticket minted by a
 * signed-in session, the player's remote client fetching the game with it, a
 * real core DLL (the libretro package's test core, built with gcc) running in
 * a host session, and its save RAM going back through PUT and coming back
 * through GET on a second run.
 *
 * Skipped without gcc, which is every CI runner. What it proves that the
 * unit tests cannot is the join: that the ticket the page mints is accepted
 * by every route the player uses, and that a save survives the round trip.
 */
func TestRetroEndToEnd(t *testing.T) {
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("no gcc on the PATH")
	}
	dllDir, err := os.MkdirTemp("", "lancast-e2ecore-")
	if err != nil {
		t.Fatal(err)
	}
	dll := filepath.Join(dllDir, "testcore.dll")
	if b, err := exec.Command(gcc, "-shared", "-O2", "-o", dll,
		filepath.Join("..", "retro", "libretro", "testdata", "testcore.c")).CombinedOutput(); err != nil {
		t.Fatalf("gcc: %v\n%s", err, b)
	}

	h := newHarness(t)
	h.secure(t, "a good long password")
	libDir := t.TempDir()
	lib, err := h.st.CreateLibrary(context.Background(), "Games", "retro", libDir)
	if err != nil {
		t.Fatal(err)
	}
	romPath := filepath.Join(libDir, "Game.tst")
	if err := os.WriteFile(romPath, []byte{77, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	platform := "gba"
	id, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: lib.ID, Path: romPath, Kind: "rom", Title: "Game", SortTitle: "game",
		Platform: &platform, Container: "tst", SizeBytes: 4, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ticket := mintTicket(t, h, id)

	play := func(frames time.Duration) []byte {
		game, err := remote.New(h.srv.URL, "", id, ticket)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := game.Download(context.Background(), t.TempDir(), nil)
		if err != nil {
			t.Fatalf("download: %v", err)
		}
		data, _ := os.ReadFile(entry)
		core, err := libretro.Open(dll)
		if err != nil {
			t.Fatal(err)
		}
		s := host.New(host.Config{
			Core: core, GamePath: entry, GameData: data, Saves: game,
			SaveStateOnStop: true,
			Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
			OnEvent: func(e host.Event) {
				if e.Kind == "error" {
					t.Errorf("session error: %+v", e)
				}
			},
		})
		done := make(chan error, 1)
		go func() { done <- s.Run(context.Background()) }()
		time.Sleep(frames)
		s.Stop()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		loaded, _ := game.LoadSRAM()
		return loaded
	}

	first := play(200 * time.Millisecond)
	if len(first) != 16 || first[2] != 77 {
		t.Fatalf("save RAM on the server after one run = % x (byte 2 is the game's first byte)", first)
	}
	/*
	 * The second run starts from the save on the server. Proved by a byte
	 * only a restore can put there: the test core sets byte 1 when A is
	 * pressed and never clears it, nothing presses A here, so a 1 in byte 1
	 * after the second run came from the save it was given. (Byte 0 would
	 * not do: it is a counter held in a static in the DLL, which survives
	 * between runs in one process whether or not anything was restored.)
	 */
	game, err := remote.New(h.srv.URL, "", id, ticket)
	if err != nil {
		t.Fatal(err)
	}
	marked := append([]byte(nil), first...)
	marked[1] = 1
	if err := game.StoreSRAM(marked); err != nil {
		t.Fatal(err)
	}
	second := play(200 * time.Millisecond)
	if len(second) != 16 || second[1] != 1 {
		t.Errorf("second run's save = % x; byte 1 should have come back from the server", second)
	}
	auto, err := h.st.GetROMSave(context.Background(), userOfTicket(t, h), id, "auto")
	if err != nil || auto.Core != "lancast-test" || auto.CoreVersion != "1.0" || auto.Previous == nil {
		t.Errorf("auto state = %+v, %v (two runs: a current copy and the one it replaced)", auto, err)
	}
}

// userOfTicket is the account the harness signed in as.
func userOfTicket(t *testing.T, h *harness) string {
	t.Helper()
	users, err := h.st.ListUsers(context.Background())
	if err != nil || len(users) == 0 {
		t.Fatalf("users: %v", err)
	}
	return users[0].ID
}
