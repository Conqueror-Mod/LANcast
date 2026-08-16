package store

import (
	"context"
	"testing"
)

/*
 * The verdict has to outlive the process that produced it.
 *
 * It began as a field on live scan progress, which is in memory: a library
 * scanned on Tuesday and looked at on Wednesday showed nothing wrong with it,
 * because the server had restarted in between. That is the wrong lifetime for a
 * warning about a property that cannot be changed.
 */
func TestShapeWarningSurvivesOnTheRow(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	want := &ShapeWarning{
		Code:    "episodes_in_movie_library",
		Message: "This library was created for films, but most files are named like TV episodes.",
		Remedy:  "Remove it and add it again as TV Shows.",
	}
	if err := st.SetShapeWarning(ctx, lib.ID, want); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShapeWarning == nil {
		t.Fatal("no warning on the library row after storing one")
	}
	if got.ShapeWarning.Code != want.Code || got.ShapeWarning.Remedy != want.Remedy {
		t.Errorf("warning = %+v, want %+v", got.ShapeWarning, want)
	}

	// And the listing, which is what the settings pane actually reads.
	libs, err := st.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].ShapeWarning == nil {
		t.Error("the listing dropped the warning the row carries")
	}
}

// A warning that outlives the condition it describes is worse than none: the
// next clean scan has to be able to take it back.
func TestShapeWarningIsCleared(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetShapeWarning(ctx, lib.ID, &ShapeWarning{Code: "x", Message: "y"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetShapeWarning(ctx, lib.ID, nil); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShapeWarning != nil {
		t.Errorf("warning = %+v, want nil after a clean rescan", got.ShapeWarning)
	}
}
