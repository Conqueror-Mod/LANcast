package serverapp

import (
	"reflect"
	"testing"

	"lancast/internal/tasks"
)

// Every background job is on the Scheduled tasks board. A job dropped from
// it would still run, unseen, which is the state this board exists to end.
func TestEveryBackgroundJobIsOnTheBoard(t *testing.T) {
	b := tasks.New()
	registerTasks(b, taskDeps{})
	want := []string{"backup", "covers", "enrich", "guide", "markers", "photos", "places", "probe", "prune", "roms", "scan", "update"}
	if got := b.IDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("board holds %v, want %v", got, want)
	}
}
