/*
 * The Libraries pane: a library, where its files live, and adding one.
 *
 * Extracted from Settings.tsx, which had grown to 1,899 lines and 29 components
 * across 12 panes. The size is not the reason — the reason is written in this
 * screen's own test files. settingsShell.test.tsx exists because "a settings
 * shell's panes were once not wired to its buttons", and settingsRules.test.tsx
 * records that "a control rendered into a pane nobody can reach is the same
 * failure this project has now made three times". A file that big makes a
 * fourth time cheaper, and this pane had just grown again to hold locations.
 *
 * Nothing here changed. It is a move, and the proof it stayed wired is that
 * settingsShell, settingsRules and libraryLocations all still pass without
 * being touched — they render the real Settings component and click through to
 * these controls.
 *
 * Follows the pattern already in this directory: AuditLog, UpdateSettings,
 * DesktopSettings and PlaybackSettings are each a component and its own CSS.
 */
import { useState } from "react";
import { ApiFailure } from "@/api/client";
import { ButtonMenu } from "./Menu";
import {
  useScanStatus,
  useStartScan,
  useRefreshLibrary,
  useReparseLibrary,
  useDeleteLibrary,
  useUpdateLibrary,
  useCreateLibrary,
  useAddRoot,
  useRemoveRoot,
  useRepointRoot,
} from "@/api/hooks";
import { LIBRARY_KINDS, kindLabel } from "@/screens/libraryConfig";
import { DirectoryPicker } from "./DirectoryPicker";
import type { Library, LibraryRoot, ReparseResult } from "@/api/types";
import "./LibrarySettings.css";

function whenScanned(ts: number | null): string {
  if (!ts) return "never scanned";
  return "scanned " + new Date(ts * 1000).toLocaleDateString();
}

/*
 * What a re-parse did, in the three cases that are genuinely different.
 *
 * "0 changed" and "nothing left to examine" are the same sentence if only
 * `changed` is reported, and they mean opposite things: the first says the
 * filenames disagree with nothing, the second says every row has already been
 * re-read. A button whose success state is indistinguishable from its no-op
 * state is the failure this project keeps finding in its own UI.
 */
function ReparseOutcome({ result }: { result: ReparseResult }) {
  if (result.examined === 0) {
    return (
      <p className="set-note">
        Every item has already been re-read. Nothing to do.
      </p>
    );
  }
  if (result.changed === 0) {
    return (
      <p className="set-note">
        Re-read {result.examined.toLocaleString()} uncertain{" "}
        {result.examined === 1 ? "item" : "items"} — all already matched their
        filenames.
      </p>
    );
  }
  return (
    <p className="set-note">
      Re-read {result.examined.toLocaleString()} uncertain{" "}
      {result.examined === 1 ? "item" : "items"} and corrected{" "}
      {result.changed.toLocaleString()}. Those are being matched again now.
    </p>
  );
}

/*
 * One place a row answers from, whichever action was asked.
 *
 * `reported` names the last action rather than the component guessing from
 * three mutation states at once. Guessing would mean deciding what to show when
 * two have finished, and the honest answer — the one you just asked for — is
 * something only the click site knows.
 *
 * Refresh metadata is the reason this exists. It was the one action with no
 * feedback at all: it answers 202 and wakes the enrich worker, so the only
 * evidence it had worked was the activity dot in the nav, which says the server
 * is busy but not with what, and not with *this* library.
 */
function RowFeedback({
  reported,
  scan,
  refresh,
  reparse,
}: {
  reported: "scan" | "refresh" | "reparse" | null;
  scan: { isError: boolean; error: unknown };
  refresh: { isPending: boolean; isSuccess: boolean; isError: boolean; error: unknown };
  reparse: { isSuccess: boolean; isError: boolean; error: unknown; data?: ReparseResult };
}) {
  if (reported === "scan" && scan.isError) {
    /*
     * A scan that was already running.
     *
     * The server answers 409 with the running scan's progress rather than the
     * error envelope every other failure uses, so there is no code and no
     * message to read — parsed as an error it says only "Conflict". The status
     * is the whole signal, which is why this branches on it (see docs/api.md).
     *
     * Mostly unreachable behind a disabled button, and not entirely: two
     * admins, or the timer starting a scan between the render and the click.
     */
    const failure = scan.error instanceof ApiFailure ? scan.error : null;
    if (failure?.status === 409) {
      return (
        <p className="set-note">
          Already scanning — the scan in progress was left to finish.
        </p>
      );
    }
    return (
      <p className="set-error">
        Could not start a scan: {(scan.error as Error).message}
      </p>
    );
  }

  if (reported === "refresh") {
    if (refresh.isPending) return <p className="set-note">Asking again…</p>;
    if (refresh.isError) {
      return (
        <p className="set-error">
          Could not refresh metadata: {(refresh.error as Error).message}
        </p>
      );
    }
    if (refresh.isSuccess) {
      // Deliberately says what will happen rather than what has: the request
      // only clears the stamps and wakes the worker, and claiming the metadata
      // was refreshed would be a lie told at the moment the work starts.
      return (
        <p className="set-note">
          Queued — every item will be matched against its provider again. Progress
          shows in the activity indicator.
        </p>
      );
    }
  }

  if (reported === "reparse") {
    if (reparse.isError) {
      return (
        <p className="set-error">
          Could not re-read filenames: {(reparse.error as Error).message}
        </p>
      );
    }
    if (reparse.isSuccess && reparse.data) {
      return <ReparseOutcome result={reparse.data} />;
    }
  }

  return null;
}

export function LibraryRow({ library }: { library: Library }) {
  const { data: status } = useScanStatus(library.id);
  const scan = useStartScan();
  const refresh = useRefreshLibrary();
  const reparse = useReparseLibrary();
  const del = useDeleteLibrary();
  const running = status?.state === "running";
  const [showIssues, setShowIssues] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [editing, setEditing] = useState(false);
  const edit = useUpdateLibrary();
  /*
   * Which action this row should be reporting on.
   *
   * The three jobs used to report in three different places, or not at all:
   * scan wrote into the subtitle, re-read filenames appended its own block
   * below the row, and refresh metadata said nothing whatsoever — it fires a
   * 202 and wakes the enrich worker, so the only sign it had worked was the
   * activity dot in the nav, which is easy to miss and says nothing about
   * *this* library.
   *
   * They are genuinely different underneath — scan is a long job with
   * progress, refresh is a trigger for a worker that reports centrally, and
   * re-read is a fast bounded pass that returns a real count — and forcing them
   * into one protocol would have made the good ones worse. What they can share
   * is where they answer. This names the last one asked, so the row has exactly
   * one place to look.
   */
  const [reported, setReported] = useState<"scan" | "refresh" | "reparse" | null>(
    null,
  );

  const skipped = status?.skipped ?? 0;
  const issues = status?.issues ?? [];

  // Media the library's kind excludes. Stated in the row itself rather than
  // hidden behind the issues toggle, because it is the answer to "why is this
  // library empty" and the person asking has no reason to expand anything —
  // the scan looked successful. The wording names both halves: what was
  // ignored, and the setting that ignored it.
  const skippedKind = status?.skipped_kind ?? 0;
  const skippedExtras = status?.skipped_extras ?? 0;
  // Live progress first, the stored row second: the in-memory one is fresher
  // during and just after a scan, and the row is what is left after a restart.
  const warning = status?.shape_warning ?? library.shape_warning;
  const excluded = library.kind === "music" ? "video" : "audio";

  // A library always has at least one location; falling back to `path` covers
  // a server older than the roots endpoint rather than an empty case.
  const roots = library.roots ?? [
    { id: 0, library_id: library.id, path: library.path, created_at: 0, item_count: 0 },
  ];
  const skippedRoots = status?.roots_skipped ?? [];

  return (
    <div className="set-lib">
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">{library.name}</div>
          <div className="set-row__sub">
            {/* One location still reads as a path, which is what it is and what
                every library was until now. Several read as a count, because
                three absolute paths on one line is not a subtitle. */}
            {roots.length > 1
              ? `${roots.length} locations`
              : (roots[0]?.path ?? library.path)}{" "}
            · {library.item_count.toLocaleString()} items ·{" "}
            {running
              ? `scanning — ${status?.files_seen ?? 0} seen`
              : whenScanned(library.scanned_at)}
            {!running && skipped > 0 && (
              <>
                {" · "}
                <button
                  className="set-issues-toggle"
                  onClick={() => setShowIssues((v) => !v)}
                >
                  {skipped.toLocaleString()} skipped
                </button>
              </>
            )}
          </div>
          {/*
            The scan's verdict on its own output.

            Rendered as the server wrote it. This used to be a sentence the
            client assembled from `episodes_in_movie_library`, which covered one
            of the two ways a library ends up the wrong kind and could not see
            the other at all: a shows library that produced no shows is not
            visible in any count the client receives, because nothing was
            skipped and nothing failed.

            Stated in the row rather than behind the issues toggle, for the same
            reason the skipped-location note is: the scan *succeeded*, so nobody
            has a reason to go looking — and the mistake is permanent, since
            kind cannot be changed after a library is created.
          */}
          {!running && warning && (
            <div className="set-row__note set-row__note--warn">
              <strong>{warning.message}</strong>
              {warning.remedy && <span>{warning.remedy}</span>}
            </div>
          )}
          {/*
            A location the scan could not read.

            Stated in the row rather than behind the issues toggle, for the
            same reason the wrong-kind warning is: the scan *succeeded* and
            looks it, while having covered less of the library than it appears
            to. Nothing else on this screen would say so.
          */}
          {!running && skippedRoots.length > 0 && (
            <div className="set-row__note">
              {status?.roots_scanned ?? 0} of {roots.length} location
              {roots.length === 1 ? "" : "s"} scanned — could not read{" "}
              {skippedRoots.map((r) => r.path).join(", ")}. Items there were
              left alone, not marked missing.
            </div>
          )}
          {/*
            Extras left out of the library (ADR 0038).

            Stated rather than silent, and for a reason the other notes share:
            this number is the difference between a library that says 1,381
            films and one that says 1,192, and somebody comparing this server
            against another has no way at all to find out where the difference
            went. "189 extras" is the entire explanation.
          */}
          {!running && skippedExtras > 0 && (
            <div className="set-row__note">
              {skippedExtras.toLocaleString()} extra
              {skippedExtras === 1 ? "" : "s"} not imported — trailers,
              featurettes, deleted scenes and sample files are not works.
            </div>
          )}
          {!running && skippedKind > 0 && (
            <div className="set-row__note">
              {skippedKind.toLocaleString()} {excluded} file
              {skippedKind === 1 ? "" : "s"} ignored — this library's type is{" "}
              {kindLabel(library.kind)}.
              {library.item_count === 0 && " A library's type cannot be changed;" +
                " remove it and add it again with the right type."}
            </div>
          )}
        </div>
        <div className="set-row__actions">
          {/* Scan stays a button. It is what this pane is for, and burying the
              common action to tidy the uncommon ones would be the wrong trade. */}
          <button
            className="set-btn"
            disabled={running || scan.isPending}
            onClick={() => {
              setReported("scan");
              scan.mutate(library.id);
            }}
          >
            {running ? "Scanning…" : "Scan"}
          </button>
          {/* Confirmation stays in the row rather than inside the menu: a
              destructive step should not be one that a stray click outside the
              menu can dismiss halfway through. */}
          {confirming ? (
            <>
              <span className="set-confirm">Remove?</span>
              <button
                className="set-btn set-btn--danger"
                disabled={del.isPending || running}
                onClick={() => del.mutate(library.id)}
              >
                Yes, remove
              </button>
              <button className="set-btn" onClick={() => setConfirming(false)}>
                Cancel
              </button>
            </>
          ) : (
            <ButtonMenu
              label={`More actions for ${library.name}`}
              className="set-btn"
              actions={[
                {
                  label: editing ? "Stop editing" : "Edit",
                  disabled: running || scan.isPending,
                  onSelect: () => {
                    edit.reset();
                    setEditing((v) => !v);
                  },
                },
                // Re-read filenames above Refresh metadata, in the order a
                // person would try them: correct the question from the filename
                // first, then ask the provider again. Refreshing first re-asks
                // the same wrong question.
                {
                  label: reparse.isPending ? "Re-reading…" : "Re-read filenames",
                  disabled: running || reparse.isPending,
                  onSelect: () => {
                    setReported("reparse");
                    reparse.mutate(library.id);
                  },
                },
                {
                  label: "Refresh metadata",
                  disabled: refresh.isPending,
                  onSelect: () => {
                    setReported("refresh");
                    refresh.mutate(library.id);
                  },
                },
                {
                  label: "Remove",
                  danger: true,
                  disabled: running,
                  onSelect: () => setConfirming(true),
                },
              ]}
            />
          )}
        </div>
      </div>
      <RowFeedback
        reported={reported}
        scan={scan}
        refresh={refresh}
        reparse={reparse}
      />
      {editing && (
        <LibraryEditor
          library={library}
          pending={edit.isPending}
          error={(edit.error as Error | null)?.message}
          onSave={(v) =>
            edit.mutate(
              { id: library.id, ...v },
              { onSuccess: () => setEditing(false) },
            )
          }
        />
      )}
      {showIssues && issues.length > 0 && (
        <ul className="set-issues">
          {issues.map((i, k) => (
            <li key={k}>
              <span className="set-issue-path">{i.path}</span>
              <span className="set-issue-reason">{i.reason}</span>
            </li>
          ))}
          {skipped > issues.length && (
            <li className="set-issue-more">
              …and {(skipped - issues.length).toLocaleString()} more
            </li>
          )}
        </ul>
      )}
    </div>
  );
}

/*
 * Editing a library: its name, and where it is.
 *
 * The type is shown and not editable, with the reason on the line rather than
 * in a tooltip — a disabled control with no explanation reads as a bug. A kind
 * decides which scanner runs, which provider is asked, and what the top level
 * of the browse is; changing it would not convert a library, it would leave one
 * describing itself as something its rows are not.
 *
 * The path is editable because the drive-letter case is real: D: became E:, or
 * a folder was renamed. Every item moves with it — matches, artwork, watch
 * positions, playlist membership — which is precisely what deleting the library
 * and adding it again would throw away.
 */
function LibraryEditor({
  library,
  pending,
  error,
  onSave,
}: {
  library: Library;
  pending: boolean;
  error?: string;
  onSave: (v: { name?: string }) => void;
}) {
  const [name, setName] = useState(library.name);
  const dirty = name.trim() !== library.name;

  return (
    <div className="set-libedit">
      <label className="set-libedit__field">
        <span>Name</span>
        <input
          className="set-input set-input--wide"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
      </label>
      <LibraryLocations library={library} />
      <p className="set-row__sub">
        Type: {kindLabel(library.kind)} — a library's type cannot be changed.
        Add the folder again as the type you want.
      </p>
      {error && <p className="set-row__note">{error}</p>}
      <div className="set-row__actions">
        <button
          className="set-btn"
          disabled={!dirty || pending || name.trim() === ""}
          onClick={() => onSave({ name: name.trim() })}
        >
          {pending ? "Saving…" : "Save name"}
        </button>
      </div>
    </div>
  );
}

/*
 * Where a library's files live (ADR 0034).
 *
 * A library used to have one folder and this used to be one text field. It has
 * locations now, and each is edited on its own line rather than through the
 * library — moving a location is per location, and a library with two has one
 * of them move while the other stays exactly where it is.
 *
 * Removing is the only destructive control on this screen that deletes rows
 * rather than files, so it says the count before it asks. The rule everywhere
 * else is that scanning marks missing and never deletes; that governs what the
 * server may *infer* from an absent drive, not what a person may ask for here,
 * and the difference is worth spelling out at the moment of asking.
 */
function LibraryLocations({ library }: { library: Library }) {
  const roots = library.roots ?? [];
  const add = useAddRoot();
  const [adding, setAdding] = useState("");
  const [confirming, setConfirming] = useState<number | null>(null);
  // Browse rather than type. An absolute server path from memory is the one
  // field on this screen a person cannot check as they go — a typo is accepted,
  // stored, and only shows up as a location that scans nothing.
  const [pickerOpen, setPickerOpen] = useState(false);

  return (
    <div className="set-roots">
      <span className="section-label">Locations</span>

      {roots.map((root) => (
        <LocationRow
          key={root.id}
          library={library}
          root={root}
          // The last location cannot go: a library with none cannot be
          // scanned, resolved or moved, and the honest way to remove it is to
          // remove the library. Disabled with the reason on the control, not
          // hidden — a missing button is a question nobody can ask.
          canRemove={roots.length > 1}
          confirming={confirming === root.id}
          onConfirm={() => setConfirming(root.id)}
          onCancel={() => setConfirming(null)}
        />
      ))}

      <div className="set-roots__add">
        <input
          className="set-input set-input--wide"
          placeholder="Add another folder…"
          value={adding}
          onChange={(e) => setAdding(e.target.value)}
        />
        <button className="set-btn" onClick={() => setPickerOpen(true)}>
          Browse…
        </button>
        <button
          className="set-btn"
          disabled={adding.trim() === "" || add.isPending}
          onClick={() =>
            add.mutate(
              { libraryID: library.id, path: adding.trim() },
              { onSuccess: () => setAdding("") },
            )
          }
        >
          {add.isPending ? "Adding…" : "Add location"}
        </button>
      </div>
      {pickerOpen && (
        <DirectoryPicker
          onSelect={(p) => {
            setAdding(p);
            setPickerOpen(false);
          }}
          onClose={() => setPickerOpen(false)}
        />
      )}
      {add.error && (
        <p className="set-row__note">{(add.error as Error).message}</p>
      )}
      <p className="set-row__sub">
        Folders cannot overlap — one inside another would be scanned twice, and
        which location a file belongs to would depend on scan order. Scan after
        adding one to pick up what is in it.
      </p>
    </div>
  );
}

function LocationRow({
  library,
  root,
  canRemove,
  confirming,
  onConfirm,
  onCancel,
}: {
  library: Library;
  root: LibraryRoot;
  canRemove: boolean;
  confirming: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const [path, setPath] = useState(root.path);
  const move = useRepointRoot();
  const remove = useRemoveRoot();
  // Opens where the location currently is, since a move is usually to a
  // sibling of it — a drive letter changed, or a folder was renamed.
  const [pickerOpen, setPickerOpen] = useState(false);
  const dirty = path.trim() !== root.path;
  const err = (move.error ?? remove.error) as Error | null;

  return (
    <div className="set-root">
      <div className="set-root__line">
        <input
          className="set-input set-input--wide"
          value={path}
          onChange={(e) => setPath(e.target.value)}
        />
        <button className="set-btn" onClick={() => setPickerOpen(true)}>
          Browse…
        </button>
        <button
          className="set-btn"
          disabled={!dirty || move.isPending}
          onClick={() =>
            move.mutate({
              libraryID: library.id,
              rootID: root.id,
              path: path.trim(),
            })
          }
        >
          {move.isPending ? "Moving…" : "Move"}
        </button>
        {confirming ? (
          <>
            <span className="set-confirm">
              Remove {root.item_count.toLocaleString()} item
              {root.item_count === 1 ? "" : "s"}?
            </span>
            <button
              className="set-btn set-btn--danger"
              disabled={remove.isPending}
              onClick={() =>
                remove.mutate({ libraryID: library.id, rootID: root.id })
              }
            >
              Yes, remove
            </button>
            <button className="set-btn" onClick={onCancel}>
              Cancel
            </button>
          </>
        ) : (
          <button
            className="set-btn set-btn--danger"
            disabled={!canRemove}
            title={
              canRemove
                ? undefined
                : "A library must keep at least one location — remove the library instead"
            }
            onClick={onConfirm}
          >
            Remove
          </button>
        )}
      </div>
      <div className="set-root__sub">
        {root.item_count.toLocaleString()} item
        {root.item_count === 1 ? "" : "s"} here. Moving keeps every one of them
        — matches, artwork and watch progress travel with the folder.
      </div>
      {pickerOpen && (
        <DirectoryPicker
          initialPath={root.path}
          onSelect={(p) => {
            setPath(p);
            setPickerOpen(false);
          }}
          onClose={() => setPickerOpen(false)}
        />
      )}
      {err && <p className="set-row__note">{err.message}</p>}
    </div>
  );
}

export function AddLibrary() {
  const create = useCreateLibrary();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  // No default. The kind cannot be changed afterwards — it decides which files
  // are scanned at all and biases movie-vs-TV matching — so it is the one field
  // here that must not be settable by inattention. It defaulted to "movie", sat
  // to the right of the Browse button, and a library named "Music" pointed at a
  // music folder was created as a movie library, which then discarded 1,592
  // tracks and reported "0 items · scanned".
  const [kind, setKind] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);

  if (!open) {
    return (
      <button className="set-btn" onClick={() => setOpen(true)}>
        + Add library
      </button>
    );
  }
  return (
    <form
      className="set-add"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate(
          { name, kind, path },
          {
            onSuccess: () => {
              setOpen(false);
              setName("");
              setPath("");
            },
          },
        );
      }}
    >
      {/*
        autoFocus because opening this form unmounts the button that opened it,
        and focus falls to the document body — the caret lands beside the first
        field rather than in it, and a keyboard user is left with focus nowhere
        (ADR 0004: focus is never invisible). Same pattern as the inline
        password reset below.
      */}
      <input
        className="set-input"
        placeholder="Library name"
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        required
      />
      <input
        className="set-input set-input--wide"
        placeholder="Folder path on the server"
        value={path}
        onChange={(e) => setPath(e.target.value)}
        required
      />
      <button className="set-btn" type="button" onClick={() => setPickerOpen(true)}>
        Browse…
      </button>
      <select
        className="set-input"
        value={kind}
        onChange={(e) => setKind(e.target.value)}
        required
        aria-label="Library type"
      >
        <option value="" disabled>
          Library type…
        </option>
        {LIBRARY_KINDS.map((k) => (
          <option key={k.value} value={k.value}>
            {k.label}
          </option>
        ))}
      </select>
      <button
        className="set-btn"
        type="submit"
        disabled={create.isPending || !kind}
      >
        Create
      </button>
      <button className="set-btn" type="button" onClick={() => setOpen(false)}>
        Cancel
      </button>
      {create.isError && (
        <span className="set-error">{(create.error as Error).message}</span>
      )}
      {pickerOpen && (
        <DirectoryPicker
          onSelect={(p) => {
            setPath(p);
            setPickerOpen(false);
          }}
          onClose={() => setPickerOpen(false)}
        />
      )}
    </form>
  );
}
