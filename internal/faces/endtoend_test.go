package faces

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * The whole chain, against real photographs — skipped unless asked for.
 *
 * Everything else in this package is exercised with a fake store and no
 * subprocess, which is what makes it run in milliseconds on any machine and in
 * CI. This one runs the actual `lancast-faces` binary over an actual folder of
 * photographs, records what comes back in an actual database, and groups it.
 *
 * It is skipped by default because it needs three things a checkout does not
 * have: a cgo build of the worker, the two ONNX models, and the runtime library
 * — together about 55MB of optional download. Guarded by environment rather
 * than a build tag so that turning it on is one line and needs no rebuild.
 *
 *	LANCAST_FACES_BIN=/path/to/lancast-faces.exe \
 *	LANCAST_FACES_MODELS=/path/to/models \
 *	LANCAST_ONNXRUNTIME=/path/to/onnxruntime.dll \
 *	LANCAST_FACES_PHOTOS="D:/My Projects/TEST LIBRARIES/TEST PICTURE LIBRARY" \
 *	go test ./internal/faces/ -run TestEndToEnd -v
 *
 * What it proves that the unit tests cannot: that the subprocess protocol
 * survives contact with a real worker, that paths round-trip through stdin and
 * back out as JSON keys, and that real embeddings cluster into more than one
 * person.
 */
func TestEndToEndOverRealPhotographs(t *testing.T) {
	bin := os.Getenv("LANCAST_FACES_BIN")
	models := os.Getenv("LANCAST_FACES_MODELS")
	photos := os.Getenv("LANCAST_FACES_PHOTOS")
	if bin == "" || models == "" || photos == "" {
		t.Skip("set LANCAST_FACES_BIN, LANCAST_FACES_MODELS and " +
			"LANCAST_FACES_PHOTOS to run the end-to-end face pass")
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "faces.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Pictures", "picture", photos)
	if err != nil {
		t.Fatal(err)
	}
	// A bounded slice of the library: enough to cluster, few enough to run in
	// under a minute.
	const want = 60
	added := 0
	err = filepath.Walk(photos, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || added >= want {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".jpg", ".jpeg", ".png":
		default:
			return nil
		}
		if _, err := st.UpsertItem(ctx, store.ScanFile{
			LibraryID: lib.ID, Kind: "photo", Path: p,
			Title: info.Name(), SortTitle: info.Name(),
			MTime: info.ModTime().Unix(),
		}); err != nil {
			return err
		}
		added++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if added == 0 {
		t.Skipf("no photographs under %s", photos)
	}
	// Deliberately not parented into a gallery. The pass selects on kind and on
	// the sensitive flag, not on hierarchy, and reaching for raw SQL to build a
	// tidier fixture would mean this package holding a *sql.DB — which the
	// store exists to prevent.

	tool := &Tool{
		Dir:       filepath.Dir(bin),
		ModelsDir: models,
		Runtime:   os.Getenv("LANCAST_ONNXRUNTIME"),
	}
	if c := tool.Capabilities(ctx); !c.Ready {
		t.Fatalf("the worker is not ready: %s", c.Reason)
	}

	w := NewWorker(st, tool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := w.Run(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	s := w.Stats()
	t.Logf("examined %d photographs, found %d faces, %d failed",
		s.Examined, s.Found, s.Failed)
	if s.Examined != added {
		t.Errorf("examined %d of %d photographs", s.Examined, added)
	}
	if s.Found == 0 {
		t.Fatal("no faces at all in a real photograph library — the subprocess " +
			"protocol or the models are wrong")
	}

	clusters, err := st.FaceClusters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, c := range clusters {
		total += c.Count
	}
	t.Logf("grouped %d faces into %d people", total, len(clusters))

	if len(clusters) < 2 {
		t.Errorf("every face landed in %d group(s); real photographs contain "+
			"more than one person", len(clusters))
	}
	// And nothing is orphaned: a face with no group is a face that can never be
	// named, which is the whole product.
	if total != s.Found {
		t.Errorf("%d faces were found but %d are in a group", s.Found, total)
	}

	// Naming survives a re-run, which is the rule everything else defers to.
	if len(clusters) > 0 {
		if err := st.NameCluster(ctx, clusters[0].ID, "Somebody"); err != nil {
			t.Fatal(err)
		}
		if err := st.ClusterLibrary(ctx, lib.ID); err != nil {
			t.Fatal(err)
		}
		after, _ := st.FaceClusters(ctx, lib.ID)
		found := false
		for _, c := range after {
			if c.ID == clusters[0].ID && c.Name != nil && *c.Name == "Somebody" {
				found = true
			}
		}
		if !found {
			t.Error("a name did not survive re-clustering over real data")
		}
	}

	fmt.Fprintf(os.Stderr, "end-to-end: %d photographs, %d faces, %d people\n",
		s.Examined, s.Found, len(clusters))
}
