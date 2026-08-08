import { useActivity } from "@/api/hooks";
import type { Activity } from "@/api/types";
import "./ActivityStrip.css";

// What the server is doing right now, in the header.
//
// The workers have always reported themselves — scan, enrich, probe, cover art
// — and none of it was anywhere a person would look. The cost of that is a
// library that is half-scanned looking broken, and a grid of blank tiles during
// artwork extraction looking like artwork that failed. Both are normal states
// wearing the costume of a fault, which is the thing this project keeps having
// to fix after the fact.
//
// **Silent when idle.** A permanent status widget teaches people to ignore it,
// and then it is worth nothing on the day it matters. Nothing is happening is
// the common case and it should look like nothing.

// summarise turns the aggregate into one line, because the header has room for
// one line. Scans come first: a scan changes what the grid contains, which is
// what someone is most likely to be staring at when it looks wrong.
function summarise(a: Activity): string | null {
  if (a.scans.length > 0) {
    const s = a.scans[0];
    const more = a.scans.length > 1 ? ` +${a.scans.length - 1}` : "";
    return `Scanning ${s.name} — ${s.files_seen.toLocaleString()} files${more}`;
  }
  if (a.probe.running) {
    return `Reading media — ${a.probe.remaining.toLocaleString()} to go`;
  }
  if (a.enrich.running) {
    return `Fetching metadata — ${a.enrich.remaining.toLocaleString()} to go`;
  }
  if (a.coverart.running) {
    return `Finding album art — ${a.coverart.remaining.toLocaleString()} to go`;
  }
  // Busy, but none of the named workers claimed it.
  //
  // Not a theoretical branch: the strip was once observed blank while the
  // server reported a worker running, and that could not be reproduced. Whether
  // that was a stale render or a field that did not line up, the honest
  // behaviour is to say *something* — `busy` is derived on the server, and a UI
  // that goes silent when it cannot name the work is worse than one that admits
  // it. It is also what will happen the first time a new worker is added and
  // nobody updates this function.
  return "Working…";
}

export function ActivityStrip() {
  const { data } = useActivity();
  if (!data || !data.busy) return null;

  const line = summarise(data);
  if (!line) return null;

  return (
    <div className="activity" role="status" aria-live="polite">
      {/* Deliberately not gold. Gold means where you are (design.md), and a
          thing that pulses in the corner whenever the server is busy would
          teach the eye that gold means "something is happening" — at which
          point the focus signal is dead. */}
      <span className="activity__mark" aria-hidden="true" />
      <span className="activity__text">{line}</span>
    </div>
  );
}
