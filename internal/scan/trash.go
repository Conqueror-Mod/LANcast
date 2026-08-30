package scan

import "fmt"

/*
 * Whether a finished scan is entitled to remove what it marked missing.
 *
 * "Scanning marks missing, never deletes" exists because an unmounted drive
 * must not destroy library data, and a setting that empties the trash after
 * every scan is precisely the shape that rule fears. It is not relaxed; it is
 * given the conditions under which the fear does not apply.
 *
 * Two of the three are already true of the scanner and are asserted rather than
 * added, because a guard that restates an existing guarantee is the one that
 * survives somebody changing the other half:
 *
 *   - A scan whose every location was unreadable **fails**, and a failed scan
 *     is never asked this question.
 *   - A location that could not be read is *skipped*, and reconciliation is
 *     per-location, so nothing under it is marked missing at all.
 *
 * The third is the real one. A location that reads fine and is *empty* — a
 * share remounted at the wrong path, a drive that came back blank — walks
 * successfully, sees nothing, and marks the whole library missing. That is a
 * true statement about the walk and a false one about the library, and it is
 * the case where emptying the trash would delete everything.
 */

// TrashVerdict is whether the trash may be emptied, and why not when it may not.
type TrashVerdict struct {
	Allowed bool
	// Reason is written for a log line rather than a person: this decision is
	// made without anybody watching, and the only place it can be read
	// afterwards is the log.
	Reason string
}

// MayEmptyTrash decides whether this scan's result can be trusted to say what
// is missing.
func MayEmptyTrash(p Progress) TrashVerdict {
	if p.State == StateFailed {
		return TrashVerdict{Reason: "the scan failed, so what is missing is not known"}
	}
	if len(p.RootsSkipped) > 0 {
		return TrashVerdict{Reason: fmt.Sprintf(
			"%d location(s) could not be read, so what is missing is not known",
			len(p.RootsSkipped))}
	}
	/*
	 * Nothing seen is the case this exists for.
	 *
	 * A library that genuinely holds no files loses nothing by being skipped —
	 * there is no trash in it either. So refusing on zero costs nothing in the
	 * honest case and saves everything in the dishonest one.
	 */
	if p.FilesSeen == 0 {
		return TrashVerdict{Reason: "the scan saw no files at all, which is a claim about the walk rather than the library"}
	}
	return TrashVerdict{Allowed: true}
}
