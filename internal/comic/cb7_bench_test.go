package comic

import (
	"os"
	"testing"
	"time"
)

// Timing a real solid .cb7, page by page, the way the API reads one: opened
// afresh for every page. Only with LANCAST_CB7 set to a file.
func TestCB7PageTimes(t *testing.T) {
	p := os.Getenv("LANCAST_CB7")
	if p == "" {
		t.Skip("set LANCAST_CB7")
	}
	probe, err := OpenBook(p)
	if err != nil {
		t.Fatal(err)
	}
	n := probe.Len()
	probe.Close()
	t.Cleanup(drainShelf)
	total := time.Now()
	for i := 0; i < n; i++ {
		start := time.Now()
		b, err := OpenBook(p)
		if err != nil {
			t.Fatal(err)
		}
		body, _, err := b.Page(i)
		b.Close()
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 || i == n/2 || i == n-1 {
			t.Logf("page %d of %d: %d KB in %v", i, n, len(body)/1024, time.Since(start).Round(time.Millisecond))
		}
	}
	t.Logf("all %d pages, one request each: %v", n, time.Since(total).Round(time.Millisecond))
	// A jump back to the start costs a restart of the stream.
	start := time.Now()
	b, _ := OpenBook(p)
	b.Page(0)
	b.Close()
	t.Logf("back to page 0: %v", time.Since(start).Round(time.Millisecond))
}

func TestCB7SequentialOnOneReader(t *testing.T) {
	p := os.Getenv("LANCAST_CB7")
	if p == "" {
		t.Skip("set LANCAST_CB7")
	}
	t.Cleanup(drainShelf)
	start := time.Now()
	b, err := OpenBook(p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for i := 0; i < b.Len(); i++ {
		if _, _, err := b.Page(i); err != nil {
			t.Fatal(err)
		}
		if i == 0 || i == b.Len()-1 {
			t.Logf("through page %d: %v", i, time.Since(start).Round(time.Millisecond))
		}
	}
}
