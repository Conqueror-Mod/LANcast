import { useEffect, useRef } from "react";
import { useSearchParams } from "react-router-dom";
import {
  useLibraries,
  useSettings,
  useUpdateSettings,
  useScanAllLibraries,
} from "@/api/hooks";
import { LibraryRow, AddLibrary } from "@/components/LibrarySettings";
import { RuleSelect } from "./shared";

/*
 * A server rule with a fixed set of answers.
 *
 * A select rather than a free number field, for the settings where the useful
 * values are few and the useless ones are harmful: a watched threshold of 3%
 * marks a library watched, and nobody wanted 3%. Where a range genuinely is a
 * range (weeks, items) the field is a number and the server validates it —
 * these are the ones where the list *is* the vocabulary.
 */
/*
 * Scanning every library, in one press.
 *
 * The capability was never missing — the rescan timer has always looped every
 * library — it was only ever unreachable by hand. On a five-library server
 * "check for new media" meant pressing Scan five times, which is the kind of
 * chore people quietly stop doing, and then wonder why nothing new appears.
 *
 * It sits with the timer rather than above the library list because it belongs
 * to the same idea: this is the whole-server half of scanning, and the timer is
 * the automatic version of exactly this button.
 *
 * The result says both halves. A sweep that started nothing because every
 * library was already scanning is indistinguishable from a sweep that did
 * nothing at all, and "Nothing to do" is the wrong answer to both.
 */
function ScanEverything() {
  const scanAll = useScanAllLibraries();
  /*
   * "Update libraries" from the tray (ADR 0080 §3).
   *
   * The tray holds no session, so it opens this pane with ?tray=scan and the
   * page, which is signed in as an administrator, does what the button below
   * does. The request is removed from the address before the scan starts, so
   * a reload or Back can never start another, and a ref keeps a re-render
   * (or React's development double effect) from doing it twice.
   */
  const [params, setParams] = useSearchParams();
  const fromTray = params.get("tray") === "scan";
  const handled = useRef(false);
  useEffect(() => {
    /*
     * Re-armed once the request has left the address. The tray now routes an
     * open window rather than reloading it, so this pane can still be mounted
     * when the next "Update libraries" arrives, and a ref that stayed set
     * would ignore it.
     */
    if (!fromTray) {
      handled.current = false;
      return;
    }
    if (handled.current) return;
    handled.current = true;
    const next = new URLSearchParams(params);
    next.delete("tray");
    setParams(next, { replace: true });
    scanAll.mutate();
  }, [fromTray, params, setParams, scanAll]);
  /*
   * Both lists defaulted rather than trusted.
   *
   * A settings pane that throws takes the whole screen with it, and the thing
   * it would be throwing over is a 202 whose body did not arrive in the shape
   * this component assumed. Reading "no libraries to scan" from an odd answer
   * is wrong in a way somebody can recover from; a white screen is not.
   */
  const started = scanAll.data?.started ?? [];
  const busy = scanAll.data?.busy ?? [];
  const answered = scanAll.isSuccess;

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Scan every library</div>
        <div className="set-row__sub">
          Walks every location for new, changed and missing files — the same
          thing the timer below does, when you would rather not wait for it.
        </div>
        {scanAll.isError && (
          <p className="set-error">
            Could not start: {(scanAll.error as Error).message}
          </p>
        )}
        {answered && (
          <p className="set-note">
            {started.length > 0 && (
              <>
                Scanning {started.length}{" "}
                {started.length === 1 ? "library" : "libraries"}.
              </>
            )}
            {started.length > 0 && busy.length > 0 && " "}
            {busy.length > 0 && (
              <>
                {busy.length} {busy.length === 1 ? "was" : "were"} already
                scanning and {busy.length === 1 ? "was" : "were"} left to
                finish.
              </>
            )}
            {started.length === 0 && busy.length === 0 && (
              <>There are no libraries to scan yet.</>
            )}
          </p>
        )}
      </div>
      <div className="set-row__actions">
        <button
          className="set-btn"
          disabled={scanAll.isPending}
          onClick={() => scanAll.mutate()}
        >
          {scanAll.isPending ? "Starting…" : "Scan all"}
        </button>
      </div>
    </div>
  );
}

export function LibrariesPane() {
  const { data: libraries } = useLibraries();
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  return (
    <section className="settings__section">
      <span className="section-label">Libraries</span>
      {libraries?.map((lib) => (
        <LibraryRow key={lib.id} library={lib} />
      ))}
      <AddLibrary />
      <ScanEverything />

      {settings && (
        <>
          <RuleSelect
            title="Rescan libraries automatically"
            sub="LANcast scans when you ask it to and when a library is added. A timer is for a server whose media arrives by other means — a downloader, a sync job, another machine writing to the drive. A library already scanning is skipped, never queued."
            value={settings.scan_interval_hours}
            options={[
              { value: 0, label: "Never" },
              { value: 1, label: "Hourly" },
              { value: 6, label: "Every 6 hours" },
              { value: 12, label: "Every 12 hours" },
              { value: 24, label: "Daily" },
              { value: 168, label: "Weekly" },
            ]}
            onChange={(v) => update.mutate({ scan_interval_hours: v })}
          />
          {/*
            Offered only once a timer is on. "Start scans at 3am" with
            scanning set to Never is a control that cannot do anything, and
            one of those invites somebody to set it and wonder why nothing
            happens.
          */}
          {settings.scan_interval_hours > 0 && (
            <>
              <label className="set-row">
                <span>Start scans at</span>
                <select
                  className="set-input"
                  value={
                    settings.scan_at_hour == null
                      ? ""
                      : String(settings.scan_at_hour)
                  }
                  disabled={update.isPending}
                  onChange={(e) =>
                    update.mutate({
                      // Null clears it; a number sets it. The empty option
                      // is "any time", which is a real answer rather than a
                      // missing one.
                      scan_at_hour:
                        e.target.value === ""
                          ? null
                          : Number(e.target.value),
                    })
                  }
                >
                  <option value="">Any time</option>
                  {Array.from({ length: 24 }, (_, h) => (
                    <option key={h} value={String(h)}>
                      {String(h).padStart(2, "0")}:00
                    </option>
                  ))}
                </select>
              </label>
              <p className="set-row__sub">
                Holds a scan back until this hour, in this server&rsquo;s own
                time. It does not change how often one is due &mdash; a
                weekly scan set to 03:00 is still weekly, it just starts at
                three in the morning rather than whenever the week happens to
                turn over. Useful because a scan competes for the same disk
                a film is being read from.
              </p>
            </>
          )}
          {/* The switch that decides whether this server can destroy media at
            all. Off is a real answer, and it was not available before:
            every install could delete files from disk through the API. */}
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.allow_media_deletion}
              onChange={(e) =>
                update.mutate({ allow_media_deletion: e.target.checked })
              }
            />
            Allow deleting media files from disk
          </label>
          <div className="set-row__sub set-row__sub--standalone">
            When off, removing a title takes it out of the library and
            leaves every file where it is. Nothing on this server can then
            delete your media.
          </div>

          {/*
            Emptying the trash, which is about *rows* and not files.
            Deliberately below the deletion switch and worded to keep the
            two apart: one destroys media, this destroys the record of media
            that is already gone.
          */}
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.empty_trash_on_scan ?? false}
              onChange={(e) =>
                update.mutate({ empty_trash_on_scan: e.target.checked })
              }
            />
            Forget missing files after a scan
          </label>
          <div className="set-row__sub set-row__sub--standalone">
            A file that has gone leaves its entry behind, marked missing, so
            a drive that failed to mount does not cost you a library. Turn
            this on and a scan removes those entries — along with their
            watch history, positions and ratings, which is what makes it
            worth asking about rather than doing quietly.
            <br />A scan that could not read a location, or that saw no
            files at all, leaves them alone whatever this says: an empty
            walk is a statement about the walk and not about the library.
          </div>
        </>
      )}
    </section>
  );
}
