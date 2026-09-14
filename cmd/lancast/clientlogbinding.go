package main

import (
	"path/filepath"

	"lancast/internal/applog"
)

/*
 * Reading the window's own log, from the window.
 *
 * The log exists so that a question asked a week later can still be answered
 * (see clientlog.go). A file in an application data directory somebody has to
 * be told how to find is most of the way to no log at all: every time it has
 * been needed so far, the next step was a message explaining where to look.
 *
 * This is a binding rather than an endpoint because the file is on *this*
 * machine. The server has never seen it, cannot fetch it, and a phone opening
 * Settings is not looking at this window's log — so the section only exists
 * where the binding does.
 */

// clientLogLines is how much is handed to the page at once. The same figure the
// server log section uses: enough to cover a start-up and whatever went wrong
// after it, short enough to render as one block of text.
const clientLogLines = 500

// clientLogBindings are the window functions behind the client log section.
func clientLogBindings(dir string) map[string]any {
	return map[string]any{
		/*
		 * lancastClientLog returns the end of this window's log.
		 *
		 * Read-only, and it takes no arguments — no filename, no directory, no
		 * line count. The page names nothing, so there is nothing for it to
		 * name wrongly: the one file this process writes is the one file it
		 * will read back, which is the same rule the games bindings keep.
		 */
		"lancastClientLog": func() map[string]any {
			if dir == "" {
				return map[string]any{"error": "no client directory"}
			}
			lines, complete, err := applog.TailNamed(dir, applog.ClientFileName, clientLogLines)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			if lines == nil {
				lines = []string{}
			}
			/*
			 * The path is reported so it can be copied into a bug report, and
			 * because a reader who wants the whole file rather than the end of
			 * it needs somewhere to go.
			 */
			return map[string]any{
				"path":     filepath.Join(dir, applog.ClientFileName),
				"lines":    lines,
				"complete": complete,
			}
		},
	}
}
