package api

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type browseResponse struct {
	Path    string        `json:"path"`
	Parent  *string       `json:"parent"`
	Entries []browseEntry `json:"entries"`
}

func TestBrowseListsRootsWithoutPath(t *testing.T) {
	h := newHarness(t)

	var res browseResponse
	decode(t, h.do(t, "GET", "/api/browse", nil), &res)

	if len(res.Entries) == 0 {
		t.Fatal("no roots returned")
	}
	if res.Parent != nil {
		t.Errorf("Parent = %v, want null at the root listing", *res.Parent)
	}
	if runtime.GOOS == "windows" {
		// At least the system drive should be present.
		var found bool
		for _, e := range res.Entries {
			if len(e.Name) == 2 && e.Name[1] == ':' {
				found = true
			}
		}
		if !found {
			t.Errorf("entries = %+v, want drive letters", res.Entries)
		}
	}
}

func TestBrowseListsSubdirectories(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Films"), 0o755)
	os.MkdirAll(filepath.Join(root, "Shows"), 0o755)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, ".hidden"), 0o755)

	var res browseResponse
	decode(t, h.do(t, "GET", "/api/browse?path="+escapePath(root), nil), &res)

	if len(res.Entries) != 2 {
		t.Fatalf("entries = %+v, want exactly the two visible directories", res.Entries)
	}
	// Sorted, case-insensitively.
	if res.Entries[0].Name != "Films" || res.Entries[1].Name != "Shows" {
		t.Errorf("entries = %+v, want Films then Shows", res.Entries)
	}
	// Files must never appear — this is a folder picker, and listing file
	// names would disclose more than the feature needs.
	for _, e := range res.Entries {
		if e.Name == "notes.txt" {
			t.Error("a file was listed")
		}
		if e.Name == ".hidden" {
			t.Error("a dotfile directory was listed")
		}
	}
	if res.Parent == nil {
		t.Error("Parent is null for a normal directory")
	}
}

func TestBrowseEntriesAreNavigable(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	nested := filepath.Join(root, "Films", "4K")
	os.MkdirAll(nested, 0o755)

	var res browseResponse
	decode(t, h.do(t, "GET", "/api/browse?path="+escapePath(root), nil), &res)
	if len(res.Entries) != 1 {
		t.Fatalf("entries = %+v", res.Entries)
	}

	// The returned path must be usable directly as the next request.
	var deeper browseResponse
	decode(t, h.do(t, "GET", "/api/browse?path="+escapePath(res.Entries[0].Path), nil), &deeper)
	if len(deeper.Entries) != 1 || deeper.Entries[0].Name != "4K" {
		t.Errorf("nested listing = %+v, want the 4K folder", deeper.Entries)
	}
}

func TestBrowseErrors(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	os.WriteFile(file, []byte("x"), 0o644)

	wantError(t, h.do(t, "GET", "/api/browse?path="+escapePath(filepath.Join(root, "nope")), nil),
		404, "not_found")
	wantError(t, h.do(t, "GET", "/api/browse?path="+escapePath(file), nil),
		400, "bad_request")
}

// The picker must be able to climb back out to the root list rather than
// stranding the user on one drive.
func TestBrowseParentAtRootReturnsEmptyString(t *testing.T) {
	h := newHarness(t)

	start := "/"
	if runtime.GOOS == "windows" {
		start = filepath.VolumeName(os.Getenv("SystemDrive")) + `\`
		if start == `\` {
			start = `C:\`
		}
	}

	resp := h.do(t, "GET", "/api/browse?path="+escapePath(start), nil)
	if resp.StatusCode != http.StatusOK {
		t.Skipf("cannot read %s in this environment", start)
	}
	var res browseResponse
	decode(t, resp, &res)

	if res.Parent == nil || *res.Parent != "" {
		t.Errorf("Parent = %v, want an empty string so Up returns to the root list", res.Parent)
	}
}

// escapePath percent-encodes a filesystem path for use in a query string.
func escapePath(p string) string {
	out := ""
	for _, r := range p {
		switch {
		case r == '\\':
			out += "%5C"
		case r == ' ':
			out += "%20"
		case r == ':':
			out += "%3A"
		default:
			out += string(r)
		}
	}
	return out
}
