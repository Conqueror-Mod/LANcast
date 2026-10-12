//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

/*
 * The tray's lessons, carried over from the server's tray it replaces
 * (ADR 0080). Read from the source, because systray owns its message loop and
 * there is no way to drive a real menu from a test.
 */

func traySrc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("desktray_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func funcBody(t *testing.T, src, name string) string {
	t.Helper()
	i := strings.Index(src, "func "+name+"(")
	if i < 0 {
		t.Fatalf("%s is gone", name)
	}
	j := strings.Index(src[i+1:], "\nfunc ")
	if j < 0 {
		return src[i:]
	}
	return src[i : i+1+j]
}

// Exit must say the service keeps running. Stopping a service needs
// elevation, and an item called Exit that raised a UAC prompt, or silently
// failed to stop anything, is the confusion the wording prevents.
func TestExitSaysAServiceKeepsRunning(t *testing.T) {
	menu := funcBody(t, traySrc(t), "buildTrayMenu")
	if !strings.Contains(menu, `AddMenuItem("Exit"`) {
		t.Fatal("there is no Exit item")
	}
	if !strings.Contains(menu, "service keeps running") {
		t.Error("Exit no longer says that a LANcast service keeps running")
	}
	for _, never := range []string{"service stop", "Stop-Service", "StopService", "runas"} {
		if strings.Contains(menu, never) {
			t.Errorf("the tray menu reaches for %q; Exit must never stop the service", never)
		}
	}
}

// One goroutine per item. systray drops a click when nothing is waiting on
// that item's channel at that instant, so one select over every item let a
// slow handler swallow the others' clicks — watched happening on the
// server's tray.
func TestEveryItemHasItsOwnWaiter(t *testing.T) {
	menu := funcBody(t, traySrc(t), "buildTrayMenu")
	if strings.Contains(menu, "select {") {
		t.Error("the menu is served by one select again; a slow handler will drop other items' clicks")
	}
	items := strings.Count(menu, "systray.AddMenuItem")
	watched := strings.Count(menu, "watch(m")
	if items == 0 || watched != items {
		t.Errorf("%d items, %d watched; every item needs its own goroutine", items, watched)
	}
}

// The login tick comes from the registry: once to decide what to do (the
// widget's own idea is stale the moment Windows ticks it), and once after
// the change to show what actually happened.
func TestTheLoginTickIsReadFromTheRegistry(t *testing.T) {
	body := funcBody(t, traySrc(t), "toggleLogin")
	first := strings.Index(body, "autostart.Enabled(")
	change := strings.Index(body, "applyAutostart(")
	last := strings.LastIndex(body, "autostart.Enabled(")
	if first < 0 || change < 0 || first == last {
		t.Fatal("toggleLogin must read the registry, change it, and read it back")
	}
	if !(first < change && change < last) {
		t.Error("the reads and the change are out of order: decide from the registry, change, then read back")
	}
	if strings.Contains(body, "item.Checked()") {
		t.Error("the intent is computed from the widget, which Windows has already toggled")
	}
}
