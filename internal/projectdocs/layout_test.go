package projectdocs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

/*
 * The layout block in CLAUDE.md describes directories that exist.
 *
 * That file is read at the start of every session, so a wrong map does not
 * mislead once — it misleads every time, and it is believed, because it is the
 * project describing itself.
 *
 * It had drifted twice and in the same direction, both times by keeping a
 * shape the code had moved away from:
 *
 *   - `internal/together/ … peer/, presence/, identity/ for other servers`,
 *     when those are three top-level packages and `internal/together` has no
 *     subdirectories at all. Acting on that line, I concluded the federation
 *     work did not exist and said so in an ADR.
 *   - `cmd/lancast/ … (clientwindow/, webview2/, certpin/)`, when all three
 *     are under `internal/`.
 *
 * This is the same kind of guard `apidoc_test.go` and `openapi_test.go` already
 * are: prose checked against the thing it claims to describe. It only asserts
 * that what the map names is really there — not that everything real is named,
 * because the map is deliberately selective and CLAUDE.md asks it to stay lean.
 */
func TestLayoutNamesDirectoriesThatExist(t *testing.T) {
	root := repoRoot(t)

	doc, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}

	block := between(string(doc), "## Layout", "## Rules")
	if block == "" {
		t.Fatal("no layout block in CLAUDE.md; if it was renamed this guard " +
			"needs to follow it rather than be deleted")
	}

	// A path at the start of a line, which is how the map lists them. Trailing
	// prose on the same line is ignored.
	paths := regexp.MustCompile(`(?m)^(internal|cmd|docs|web)/[a-zA-Z0-9_/]+/`)
	found := paths.FindAllString(block, -1)
	if len(found) == 0 {
		t.Fatal("the layout block lists no directories, which cannot be right")
	}

	seen := map[string]bool{}
	for _, p := range found {
		if seen[p] {
			continue
		}
		seen[p] = true
		if info, err := os.Stat(filepath.Join(root, p)); err != nil || !info.IsDir() {
			t.Errorf("CLAUDE.md's layout names %s, which does not exist.\n\n"+
				"That file is read at the start of every session, so a wrong map "+
				"is believed every time. Point it at the real directory rather "+
				"than removing the line.", p)
		}
	}
}

// between returns the text from one heading to the next, exclusive.
func between(doc, start, end string) string {
	i := strings.Index(doc, start)
	if i < 0 {
		return ""
	}
	rest := doc[i+len(start):]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

// repoRoot walks up until it finds go.mod, so the test does not care where it
// is run from or how deep this package sits.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}
