package projectdocs

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

/*
 * The README's status line and its count of decision records match the repo.
 *
 * The README is the first thing a stranger reads, and it is read as the
 * project describing itself. On 2026-10-25 an outside review read it that way:
 * the status line said v0.8.34 while v0.9.94 was current, and the docs table
 * said "50 decision records" when there were 92. The reviewer filed three
 * features as missing that had shipped months earlier, because the rest of the
 * docs had drifted the same way and the README gave no hint that they had.
 *
 * Both numbers are cheap to check and are exactly the ones nobody remembers to
 * bump. This does not check what the README *says* about features; only the two
 * facts that can be read off the repository.
 *
 * A release commits its notes as docs/releases/v<version>.md before the tag
 * (see docs/releases/README.md), so this fails in the release commit itself
 * until the README's status line is bumped with it. That is the point.
 */

var semverName = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)\.md$`)

func TestReadmeStatusIsTheLatestRelease(t *testing.T) {
	root := repoRoot(t)
	readme := readFile(t, filepath.Join(root, "README.md"))

	m := regexp.MustCompile(`\*\*Status: (v\d+\.\d+\.\d+)\b`).FindStringSubmatch(readme)
	if m == nil {
		t.Fatal("no `**Status: vX.Y.Z` line in README.md; if it was reworded " +
			"this guard needs to follow it rather than be deleted")
	}
	claimed := m[1]

	latest := latestRelease(t, filepath.Join(root, "docs", "releases"))
	if claimed != latest {
		t.Errorf("README.md's status line says %s, but the newest release notes "+
			"are docs/releases/%s.md.\n\nBump the status line in the same commit "+
			"as the release notes; a stale version is the first thing a "+
			"stranger reads.", claimed, latest)
	}
}

func TestReadmeCountsTheDecisionRecords(t *testing.T) {
	root := repoRoot(t)
	readme := readFile(t, filepath.Join(root, "README.md"))

	m := regexp.MustCompile(`\| (\d+) decision records`).FindStringSubmatch(readme)
	if m == nil {
		t.Fatal("no `| N decision records` row in README.md's docs table; if it " +
			"was reworded this guard needs to follow it rather than be deleted")
	}
	claimed, _ := strconv.Atoi(m[1])

	entries, err := os.ReadDir(filepath.Join(root, "docs", "adr"))
	if err != nil {
		t.Fatalf("read docs/adr: %v", err)
	}
	record := regexp.MustCompile(`^\d{4}-.+\.md$`)
	actual := 0
	for _, e := range entries {
		if !e.IsDir() && record.MatchString(e.Name()) {
			actual++
		}
	}
	if actual == 0 {
		t.Fatal("docs/adr holds no NNNN-*.md records, which cannot be right")
	}

	if claimed != actual {
		t.Errorf("README.md says %d decision records; docs/adr holds %d.\n\n"+
			"Update the number in the docs table when adding an ADR.",
			claimed, actual)
	}
}

// latestRelease returns the highest vX.Y.Z among the release-notes files,
// compared numerically so v0.9.100 sorts after v0.9.99.
func latestRelease(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var best [3]int
	name := ""
	for _, e := range entries {
		m := semverName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		var v [3]int
		for i := range v {
			v[i], _ = strconv.Atoi(m[i+1])
		}
		if name == "" || newer(v, best) {
			best, name = v, e.Name()[:len(e.Name())-len(".md")]
		}
	}
	if name == "" {
		t.Fatalf("no v<version>.md files in %s", dir)
	}
	return name
}

func newer(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
