package webcores

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

/*
 * The real archive, opt in.
 *
 * Set LANCAST_WEBCORES_ARCHIVE to a downloaded copy of the pinned RetroArch.7z
 * and this serves it locally and installs from it with the real pins: the
 * archive's own SHA-256, then each of the eight files against its own. It is
 * how the pins were confirmed on 2026-10-11, and how they should be confirmed
 * again whenever they move to a new release. Skipped otherwise, because the
 * archive is 749 MB and the test needs no network to stay that way.
 */
func TestInstallFromTheRealArchive(t *testing.T) {
	path := os.Getenv("LANCAST_WEBCORES_ARCHIVE")
	if path == "" {
		t.Skip("LANCAST_WEBCORES_ARCHIVE is not set")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}))
	defer srv.Close()

	a := Archive
	a.URL = srv.URL + "/RetroArch.7z"
	dir := t.TempDir()
	start := time.Now()
	if err := install(context.Background(), a, dir, all, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("installed from the real archive in %s", time.Since(start).Round(time.Second))
	for _, c := range all {
		if !c.Present(dir) {
			t.Errorf("%s is not present after install", c.Name)
		}
	}
}
