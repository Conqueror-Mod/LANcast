package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/optimize"
	"lancast/internal/probe"
	"lancast/internal/store"
)

/*
 * Optimized copies at the boundary (ADR 0087). The conversion itself needs
 * ffmpeg and is proven as the installed service; what is proven here is
 * everything around it: who may ask, what a container expands to, when
 * playback switches to the copy and when it must not, and that a copy taken
 * away takes its file with it.
 */

// readyCopy makes a finished copy for id as the worker would leave it: a row
// marked ready with the copy's probe, and the file in the optimized folder.
func readyCopy(t *testing.T, h *harness, id int64, body string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := h.st.RequestOptimized(ctx, []int64{id}); err != nil {
		t.Fatal(err)
	}
	res := probe.Result{Container: "mov", BitRate: 7_000_000, Streams: []probe.Stream{
		{Index: 0, Kind: probe.KindVideo, Codec: "h264", Width: 1920, Height: 1080, PixFmt: "yuv420p"},
		{Index: 1, Kind: probe.KindAudio, Codec: "aac", Channels: 2},
	}}
	raw, _ := json.Marshal(res)
	if ok, err := h.st.MarkOptimized(ctx, id, store.OptimizedReady, int64(len(body)), string(raw), ""); !ok || err != nil {
		t.Fatalf("mark ready: %v %v", ok, err)
	}
	path, err := h.srvAPI.optimizedPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPlaybackSwitchesToTheCopyOnlyWhenItHelps(t *testing.T) {
	h := langHarness(t)
	id := h.addFile(t, "uhd.mkv", []byte("original"))
	h.probeAsHEVCWithTwoAudioTracks(t, id)

	type answer struct {
		Version  string         `json:"version"`
		Decision probe.Decision `json:"decision"`
	}
	ask := func(query string) answer {
		var a answer
		decode(t, h.authed(t, "GET", fmt.Sprintf("/api/items/%d/playback%s", id, query), nil), &a)
		return a
	}

	// No copy yet: a browser has to convert 4K HEVC TrueHD.
	if a := ask(""); a.Version != "" || a.Decision.Method == probe.DirectPlay {
		t.Fatalf("before the copy = %+v", a)
	}

	path := readyCopy(t, h, id, "the copy")
	a := ask("")
	if a.Version != "optimized" || a.Decision.Method != probe.DirectPlay {
		t.Fatalf("with a copy = %+v, want direct play of the copy", a)
	}
	resp := h.authed(t, "GET", fmt.Sprintf("/api/stream/%d?version=optimized", id), nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "the copy" || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Errorf("the copy's stream = %d %q %s", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	// Without the version, the original as always.
	resp = h.authed(t, "GET", fmt.Sprintf("/api/stream/%d", id), nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "original" {
		t.Errorf("the plain stream served %q, want the original", body)
	}

	// A different audio track keeps the original: the copy has one.
	if a := ask("?audio=2"); a.Version != "" {
		t.Errorf("with audio=2 = %+v, want the original", a)
	}
	// Naming the default track is not choosing another: the player always
	// sends ?audio=, and the copy was made from the default (index 1 here).
	if a := ask("?audio=1"); a.Version != "optimized" {
		t.Errorf("with the default track named = %+v, want the copy", a)
	}

	// Taking the copy away removes its file at the next sweep.
	if code := status(t, h.authed(t, "DELETE", fmt.Sprintf("/api/items/%d/optimized", id), nil)); code != http.StatusNoContent {
		t.Fatalf("remove = %d", code)
	}
	h.srvAPI.sweepOptimized(context.Background())
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the copy's file survived its removal: %v", err)
	}
	if a := ask(""); a.Version != "" {
		t.Errorf("after removal = %+v, still the copy", a)
	}
}

func TestOnlyAnAdministratorAsksForCopiesAndAShowExpands(t *testing.T) {
	a := newAccessHarness(t)
	ctx := context.Background()
	show, err := a.st.UpsertItem(ctx, store.ScanFile{LibraryID: a.lib.ID, Path: filepath.Join(a.dir, "Show"),
		Kind: "show", Title: "Show", SortTitle: "Show", Container: "", SizeBytes: 0, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	season, _ := a.st.UpsertItem(ctx, store.ScanFile{LibraryID: a.lib.ID, Path: filepath.Join(a.dir, "Show", "S1"),
		Kind: "season", Title: "Season 1", SortTitle: "Season 1", MTime: 1})
	_ = a.st.SetParent(ctx, season, &show)
	for i := 1; i <= 3; i++ {
		ep, _ := a.st.UpsertItem(ctx, store.ScanFile{LibraryID: a.lib.ID,
			Path: filepath.Join(a.dir, "Show", "S1", fmt.Sprintf("e%d.mkv", i)), Kind: "episode",
			Title: fmt.Sprintf("Episode %d", i), SortTitle: fmt.Sprintf("Episode %d", i), Container: "mkv", SizeBytes: 1, MTime: 1})
		_ = a.st.SetParent(ctx, ep, &season)
	}

	path := fmt.Sprintf("/api/items/%d/optimized", show)
	if code := status(t, a.doAs(t, a.member, "PUT", path, nil)); code != http.StatusForbidden {
		t.Errorf("member PUT = %d, want 403", code)
	}
	var got struct {
		Queued int `json:"queued"`
		Titles int `json:"titles"`
	}
	decode(t, a.authed(t, "PUT", path, nil), &got)
	if got.Queued != 3 || got.Titles != 3 {
		t.Errorf("a show of three episodes queued %+v", got)
	}
	var list struct {
		Items      []store.OptimizedVersion `json:"items"`
		LimitBytes int64                    `json:"limit_bytes"`
	}
	decode(t, a.authed(t, "GET", "/api/optimized", nil), &list)
	if len(list.Items) != 3 || list.LimitBytes != 200<<30 {
		t.Errorf("list = %d items, limit %d", len(list.Items), list.LimitBytes)
	}
}

// The folder may not be inside a library: a scan would find the copies.
func TestTheOptimizedFolderMayNotBeInsideALibrary(t *testing.T) {
	h := langHarness(t)
	inside := filepath.Join(h.dir, "optimized")
	if code := status(t, h.authed(t, "PUT", "/api/settings", map[string]any{"optimized_dir": inside})); code != http.StatusBadRequest {
		t.Errorf("a folder inside the library = %d, want 400", code)
	}
	outside := filepath.Join(t.TempDir(), "optimized")
	if code := status(t, h.authed(t, "PUT", "/api/settings", map[string]any{"optimized_dir": outside})); code != http.StatusOK {
		t.Errorf("a folder outside = %d, want 200", code)
	}
	if got, _ := h.srvAPI.optimizedPath(7); filepath.Dir(got) != outside || filepath.Base(got) != optimize.FileName(7) {
		t.Errorf("copy path = %s", got)
	}
}

/*
 * Only a copy that can be served is offered (R03-2).
 *
 * The playback answer read the row and never the file, so moving the folder
 * left every device sent to ?version=optimized, which answered 503 from the
 * new folder, and the live conversion that would have worked was never
 * offered. Changing the folder now re-queues each ready copy it does not
 * hold; a copy moved there by hand is found and kept; and a file deleted by
 * hand is caught the next time it would have been offered.
 */
func TestMovingTheOptimizedFolderOffersNothingItCannotServe(t *testing.T) {
	h := langHarness(t)
	ctx := context.Background()
	moved := h.addFile(t, "uhd.mkv", []byte("original"))
	h.probeAsHEVCWithTwoAudioTracks(t, moved)
	kept := h.addFile(t, "uhd2.mkv", []byte("original two"))
	h.probeAsHEVCWithTwoAudioTracks(t, kept)
	readyCopy(t, h, moved, "the copy")
	keptOld := readyCopy(t, h, kept, "the other copy")

	type answer struct {
		Version  string         `json:"version"`
		Decision probe.Decision `json:"decision"`
	}
	ask := func(id int64) answer {
		var a answer
		decode(t, h.authed(t, "GET", fmt.Sprintf("/api/items/%d/playback", id), nil), &a)
		return a
	}
	if a := ask(moved); a.Version != "optimized" {
		t.Fatalf("before the move = %+v, want the copy (the control)", a)
	}

	// Somebody carries one copy across by hand before changing the setting.
	newDir := filepath.Join(t.TempDir(), "optimized")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(keptOld, filepath.Join(newDir, optimize.FileName(kept))); err != nil {
		t.Fatal(err)
	}
	if code := status(t, h.authed(t, "PUT", "/api/settings", map[string]any{"optimized_dir": newDir})); code != http.StatusOK {
		t.Fatalf("change the folder = %d", code)
	}

	// The copy left behind is no longer offered; whatever version playback
	// names must be one the stream route serves.
	a := ask(moved)
	if a.Version == "optimized" {
		t.Errorf("after the move playback still offers the copy: %+v", a)
	}
	if a.Decision.Method == probe.DirectPlay {
		t.Errorf("after the move playback offers direct play of the 4K original: %+v", a)
	}
	for _, id := range []int64{moved, kept} {
		if ask(id).Version != "optimized" {
			continue
		}
		resp := h.authed(t, "GET", fmt.Sprintf("/api/stream/%d?version=optimized", id), nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("item %d: playback offers the copy and its stream answers %d", id, resp.StatusCode)
		}
	}
	// The copy moved by hand is still offered, and served.
	if a := ask(kept); a.Version != "optimized" {
		t.Errorf("a copy moved into the new folder = %+v, want it kept", a)
	}

	// The one left behind is queued to be rebuilt, and no longer counted.
	if _, err := h.st.ReadyOptimized(ctx, moved); err == nil {
		t.Error("the copy left in the old folder is still marked ready")
	}
	all, err := h.st.ListOptimized(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range all {
		if v.ItemID == moved && v.State != store.OptimizedQueued {
			t.Errorf("the copy left behind is %q, want queued to be rebuilt", v.State)
		}
	}
	if n, _ := h.st.OptimizedBytes(ctx); n != int64(len("the other copy")) {
		t.Errorf("space used = %d, want only the copy that is there", n)
	}
}

func TestACopyDeletedByHandIsNotOffered(t *testing.T) {
	h := langHarness(t)
	id := h.addFile(t, "uhd.mkv", []byte("original"))
	h.probeAsHEVCWithTwoAudioTracks(t, id)
	path := readyCopy(t, h, id, "the copy")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var a struct {
		Version string `json:"version"`
	}
	decode(t, h.authed(t, "GET", fmt.Sprintf("/api/items/%d/playback", id), nil), &a)
	if a.Version == "optimized" {
		t.Error("playback offers a copy whose file is gone")
	}
	if _, err := h.st.ReadyOptimized(context.Background(), id); err == nil {
		t.Error("a copy whose file is gone is still marked ready, not queued to be rebuilt")
	}
}
