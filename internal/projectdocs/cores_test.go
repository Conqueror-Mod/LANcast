package projectdocs

import (
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/retro/cores"
	"lancast/internal/retro/webcores"
)

/*
 * docs/cores.md lists every emulator core LANcast plays with, its licence and
 * its source. RetroAchievements asks an emulator it approves to publish that
 * list (ADR 0112), and a public list that has drifted from the code is worse
 * than none: it is the project vouching for a licence it no longer uses.
 *
 * So each core the code can fetch must have a row naming it, its licence and
 * its source, and a desktop core's row its pinned SHA-256 too.
 */
func TestTheCoreListMatchesTheCode(t *testing.T) {
	doc := readFile(t, filepath.Join(repoRoot(t), "docs", "cores.md"))
	desktop, browser, ok := strings.Cut(doc, "## The browser player")
	if !ok {
		t.Fatal(`docs/cores.md has no "## The browser player" section`)
	}
	row := func(section, display string) string {
		for _, line := range strings.Split(section, "\n") {
			if strings.HasPrefix(line, "| ") && strings.Contains(line, "| "+display+" |") {
				return line
			}
		}
		return ""
	}
	for _, c := range cores.All() {
		r := row(desktop, c.Display)
		switch {
		case r == "":
			t.Errorf("desktop core %s (%s) is not in docs/cores.md", c.Display, c.Name)
		case !strings.Contains(r, "| "+c.Licence+" |"), !strings.Contains(r, c.SourceURL), !strings.Contains(r, c.SHA256):
			t.Errorf("desktop core %s: the row says %q; the code says %s, %s, %s", c.Display, r, c.Licence, c.SourceURL, c.SHA256)
		}
	}
	for _, c := range webcores.All() {
		r := row(browser, c.Display)
		switch {
		case r == "":
			t.Errorf("browser core %s (%s) is not in docs/cores.md", c.Display, c.Name)
		case !strings.Contains(r, "| "+c.Licence+" |"), !strings.Contains(r, c.SourceURL):
			t.Errorf("browser core %s: the row says %q; the code says %s, %s", c.Display, r, c.Licence, c.SourceURL)
		}
	}
}
