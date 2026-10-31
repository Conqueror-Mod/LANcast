package comic

import (
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/bodgit/sevenzip"
)

/*
 * A .cb7 is a 7-Zip archive of page images (ADR 0101, amended): the same
 * pages as a .cbz, by the same rules, in the same order. Read with the 7-Zip
 * reader the retro cores already use, so it costs no new dependency.
 *
 * # Why it is kept open
 *
 * A 7-Zip archive is usually solid: its files are compressed as one stream,
 * so a file can only be reached by decompressing every file stored before it.
 * Opened afresh for each request, as a .cbz is, page n costs everything
 * before it. Measured on a real 44-page, 48 MB issue repacked solid: page 0
 * in 71 ms, page 22 in 2.2 s, page 43 in 4.4 s, and a reader paging through
 * would pay all of that again for every page.
 *
 * The same archive read in order on **one** reader took 4.35 s for all 44
 * pages, about 100 ms each, because the reader keeps its place in the stream.
 * So a .cb7 stays open between requests: the last two read are shared, each
 * closed after five idle minutes, and a page is read under the book's lock so
 * that one request cannot move the stream out from under another.
 *
 * Pages the stream passes on its way to the one asked for are held, up to
 * heldBudget, because the archive's order need not be the reading order
 * (p10 is stored before p2) and a reader asks for the next page next.
 */

const (
	// shelfSize is how many .cb7 archives stay open at once.
	shelfSize = 2
	// shelfIdle is how long an archive nobody is reading stays open.
	shelfIdle = 5 * time.Minute
	// heldBudget bounds the pages one archive keeps from passing them.
	heldBudget = 96 << 20
)

type cb7Archive struct {
	path string
	mod  time.Time
	size int64

	mu     sync.Mutex
	f      *os.File // opened so its owner can still delete it (openShared)
	r      *sevenzip.Reader
	files  []*sevenzip.File // in stored order
	pages  []int            // page n is files[pages[n]]
	next   int              // the stored index the stream is positioned at
	held   map[int][]byte   // pages read in passing, by stored index
	heldN  int
	refs   int
	used   time.Time
	stale  bool // replaced on the shelf; closed once nobody holds it
	closed bool
}

type cb7Shelf struct {
	mu   sync.Mutex
	open []*cb7Archive
}

var shelf = &cb7Shelf{}

// cb7 is one caller's hold on a shared archive.
type cb7 struct {
	a    *cb7Archive
	once sync.Once
}

func openCB7(path string) (Book, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("comic: %w", err)
	}
	a, err := shelf.take(path, info)
	if err != nil {
		return nil, err
	}
	return &cb7{a: a}, nil
}

// take returns the open archive for path, opening it if the shelf has none or
// has one for a file since replaced.
func (s *cb7Shelf) take(path string, info os.FileInfo) (*cb7Archive, error) {
	s.mu.Lock()
	for i, a := range s.open {
		if a.path != path {
			continue
		}
		if a.mod.Equal(info.ModTime()) && a.size == info.Size() {
			a.refs++
			a.used = time.Now()
			s.mu.Unlock()
			return a, nil
		}
		// The file changed under it: this one goes once it is let go.
		s.open = append(s.open[:i], s.open[i+1:]...)
		a.stale = true
		s.closeIfIdleLocked(a)
		break
	}
	s.mu.Unlock()

	a, err := openArchive(path, info)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	a.refs = 1
	a.used = time.Now()
	s.open = append([]*cb7Archive{a}, s.open...)
	// Over the limit: close the least recently used that nobody holds.
	for len(s.open) > shelfSize {
		evicted := false
		for i := len(s.open) - 1; i >= 0; i-- {
			if old := s.open[i]; old.refs == 0 {
				s.open = append(s.open[:i], s.open[i+1:]...)
				old.stale = true
				s.closeIfIdleLocked(old)
				evicted = true
				break
			}
		}
		if !evicted {
			break // all in use; they are closed as they are let go
		}
	}
	return a, nil
}

// give lets go of a hold, and closes the archive after shelfIdle if nothing
// takes it again, or at once if it has left the shelf.
func (s *cb7Shelf) give(a *cb7Archive) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.refs--
	a.used = time.Now()
	if a.stale {
		s.closeIfIdleLocked(a)
		return
	}
	if a.refs == 0 {
		at := a.used
		time.AfterFunc(shelfIdle, func() { s.expire(a, at) })
	}
}

func (s *cb7Shelf) expire(a *cb7Archive, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.refs > 0 || !a.used.Equal(at) || a.closed {
		return
	}
	for i, o := range s.open {
		if o == a {
			s.open = append(s.open[:i], s.open[i+1:]...)
			break
		}
	}
	a.stale = true
	s.closeIfIdleLocked(a)
}

func (s *cb7Shelf) closeIfIdleLocked(a *cb7Archive) {
	if a.refs > 0 || a.closed {
		return
	}
	a.closed = true
	_ = a.f.Close()
}

func openArchive(path string, info os.FileInfo) (*cb7Archive, error) {
	f, err := openShared(path)
	if err != nil {
		return nil, fmt.Errorf("comic: %w", err)
	}
	r, err := sevenzip.NewReader(f, info.Size())
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("comic: %w", err)
	}
	a := &cb7Archive{path: path, mod: info.ModTime(), size: info.Size(), f: f, r: r, files: r.File, held: map[int][]byte{}}
	for i, f := range r.File {
		if isPageName(f.Name, f.FileInfo().IsDir()) {
			a.pages = append(a.pages, i)
		}
	}
	if len(a.pages) == 0 {
		f.Close()
		return nil, ErrNoPages
	}
	if len(a.pages) > MaxPages {
		f.Close()
		return nil, fmt.Errorf("comic: %d pages is more than %d", len(a.pages), MaxPages)
	}
	sort.SliceStable(a.pages, func(i, j int) bool {
		return naturalLess(r.File[a.pages[i]].Name, r.File[a.pages[j]].Name)
	})
	return a, nil
}

func (b *cb7) Len() int { return len(b.a.pages) }

func (b *cb7) Page(n int) ([]byte, string, error) {
	a := b.a
	if n < 0 || n >= len(a.pages) {
		return nil, "", fmt.Errorf("comic: no page %d", n)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	want := a.pages[n]
	name := a.files[want].Name
	if body, ok := a.held[want]; ok {
		delete(a.held, want)
		a.heldN -= len(body)
		return body, ContentType(name), nil
	}
	// Ahead of the stream: keep the pages it passes, within the budget.
	if want >= a.next {
		isPage := make(map[int]bool, len(a.pages))
		for _, i := range a.pages {
			isPage[i] = true
		}
		for k := a.next; k < want; k++ {
			if !isPage[k] || a.held[k] != nil || a.heldN+int(a.files[k].UncompressedSize) > heldBudget {
				continue
			}
			body, err := readEntry(a.files[k])
			if err != nil {
				break // the page asked for is still read below
			}
			a.held[k] = body
			a.heldN += len(body)
		}
	}
	body, err := readEntry(a.files[want])
	if err != nil {
		return nil, "", err
	}
	a.next = want + 1
	return body, ContentType(name), nil
}

func (b *cb7) Close() error {
	b.once.Do(func() { shelf.give(b.a) })
	return nil
}

// readEntry reads one stored file, refusing one bigger than MaxPageBytes.
func readEntry(f *sevenzip.File) ([]byte, error) {
	if f.UncompressedSize > MaxPageBytes {
		return nil, fmt.Errorf("comic: page %s is too large", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("comic: %w", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, MaxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("comic: %w", err)
	}
	if len(body) > MaxPageBytes {
		return nil, fmt.Errorf("comic: page %s is too large", f.Name)
	}
	return body, nil
}
