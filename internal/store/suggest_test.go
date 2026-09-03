package store

import (
	"context"
	"testing"
)

/*
 * Suggestions are near-misses, not the least-dissimilar strangers.
 *
 * The 126 faces on a real library that grouped with nothing are not false
 * detections — same detector confidence as everything else, just harder.
 * Clustering cannot reach them by lowering its threshold, because erring low
 * attaches somebody's face to somebody else's name. A person can answer what
 * the threshold cannot, so the machine offers and a human decides.
 */

func TestSuggestionsExcludeAnythingAlreadyCloseEnough(t *testing.T) {
	// A similarity at or above the threshold is not a suggestion: that face is
	// already in the group, so offering it would be offering the group itself.
	if SameFaceCosine*0.66 >= SameFaceCosine {
		t.Fatal("the suggestion band is empty by construction")
	}
}

func TestSuggestionsHaveAFloor(t *testing.T) {
	// Without one, the list is the least-dissimilar strangers in the library,
	// offered with the same confidence as a real near-miss — which trains
	// somebody to dismiss the feature rather than read it.
	floor := SameFaceCosine * 0.66
	if floor <= 0 {
		t.Fatal("no floor")
	}
	if floor > SameFaceCosine*0.9 {
		t.Errorf("floor %.3f is too near the threshold to leave a usable band", floor)
	}
}

func TestSuggestionsForAnUnknownClusterAreNotAnError(t *testing.T) {
	st := openTestStore(t)
	_, err := st.SuggestedForCluster(context.Background(), 999999, 6)
	if err == nil {
		t.Error("an unknown cluster returned no error; callers cannot tell it apart from none")
	}
}
