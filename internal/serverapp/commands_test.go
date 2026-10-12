package serverapp

import (
	"os"
	"reflect"
	"regexp"
	"testing"
)

func TestStripServe(t *testing.T) {
	got, ok := stripServe([]string{"LANcast.exe", "serve", "-addr", ":9090"})
	if !ok || !reflect.DeepEqual(got, []string{"LANcast.exe", "-addr", ":9090"}) {
		t.Fatalf("got %v %v", got, ok)
	}
	// Bare serve must still be a serve, never a double-click.
	got, ok = stripServe([]string{"LANcast.exe", "serve"})
	if !ok || len(got) != 1 {
		t.Fatalf("bare serve: %v %v", got, ok)
	}
	if _, ok := stripServe([]string{"LANcast.exe", "-addr", ":9090"}); ok {
		t.Fatal("flags alone were read as serve")
	}
	if _, ok := stripServe([]string{"LANcast.exe"}); ok {
		t.Fatal("a bare launch was read as serve")
	}
}

/*
 * Every subcommand Main dispatches must be one IsCommand admits.
 *
 * The single executable asks IsCommand before handing a launch over, so a
 * subcommand added to Main and forgotten here would, launched through it,
 * open the desktop window instead of doing what it was asked. Read from the
 * source rather than listed twice, so the two cannot drift apart unnoticed.
 */
func TestIsCommandCoversEveryDispatchedSubcommand(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, m := range regexp.MustCompile(`os\.Args\[1\] == "([a-z-]+)"`).FindAllSubmatch(src, -1) {
		seen++
		if cmd := string(m[1]); !IsCommand(cmd) {
			t.Errorf("Main dispatches %q but IsCommand does not admit it", cmd)
		}
	}
	if !IsCommand(relaunchArg) || !IsCommand("serve") {
		t.Error("relaunch and serve must be admitted")
	}
	if seen < 5 {
		t.Fatalf("found only %d dispatches in main.go; the pattern no longer matches the source", seen)
	}
	for _, notOurs := range []string{"", "-addr", "-browser", "window", "help"} {
		if IsCommand(notOurs) {
			t.Errorf("%q was claimed by the server program", notOurs)
		}
	}
}
