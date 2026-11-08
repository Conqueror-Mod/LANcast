import { formatBytes } from "@/lib/format";
import {
  useSettings,
  useUpdateSettings,
  useFaceCapabilities,
  useFaceModels,
  useSemanticCapabilities,
  useSemanticModels,
  useInstallSemanticModels,
  useCancelSemanticModels,
  useInstallFaceModels,
  useCancelFaceModels,
} from "@/api/hooks";

/*
 * Everything a picture library can be told to do (ADR 0051, ADR 0052).
 *
 * Its own pane because there are now two of these and there will be more —
 * sensitivity, faces, and eventually whatever comes of dates, duplicates and
 * places. A general pane holding two picture settings is one somebody has to
 * read all of to find either.
 */
export function PicturesSection() {
  const { data: settings } = useSettings();
  const update = useUpdateSettings();
  const { data: caps } = useFaceCapabilities();
  const { data: models } = useFaceModels();
  const install = useInstallFaceModels();
  const cancel = useCancelFaceModels();

  const job = models?.job;
  const running = job?.running ?? false;
  const pct =
    job && job.bytes_total > 0
      ? Math.min(100, Math.round((job.bytes_done / job.bytes_total) * 100))
      : 0;

  return (
    <section className="settings__section">
      <span className="section-label">Pictures</span>

      {settings && (
        <>
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.sensitive_marking}
              onChange={(e) =>
                update.mutate({ sensitive_marking: e.target.checked })
              }
            />
            Allow folders to be marked sensitive
          </label>
          <p className="set-row__sub">
            Right-click a folder in a picture library to mark it. Anything
            marked is covered — its name still shows — and can only be uncovered
            inside the library or the folder itself, never from the home page.
            It covers again when you leave. Turning this off stops the covering
            and keeps the marks.
          </p>

          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.photo_places}
              onChange={(e) =>
                update.mutate({ photo_places: e.target.checked })
              }
            />
            Read where photos were taken
          </label>
          <p className="set-row__sub">
            Groups a picture library by the town each photograph was taken in,
            from the location a phone or camera writes into it. The towns come
            from a list built into LANcast, so nothing is looked up online.
            Turning this off deletes every location it read.
          </p>
        </>
      )}

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Group faces</div>
          <div className="set-row__sub">
            Find the people in a picture library and let you name them. It runs
            entirely on this machine — nothing is uploaded and no account is
            needed.
          </div>
        </div>
      </div>

      {/*
        Three states, said apart. "Not installed", "no models" and "ready" have
        different fixes, and a single "unavailable" sends somebody looking in
        the wrong place.
      */}
      {models && !models.supported && (
        <p className="set-row__sub">
          {models.reason ??
            "There is no face-model download for this platform yet."}
        </p>
      )}

      {models?.supported && !models.installed && !running && (
        <>
          <p className="set-row__sub">
            This needs a one-off download of{" "}
            <strong>{formatBytes(models.bytes_total ?? 0)}</strong> — two models
            and the runtime that executes them. Nothing is fetched until you
            press the button.
          </p>
          <ul className="set-assets">
            {(models.assets ?? []).map((a) => (
              <li key={a.name}>
                <code>{a.name}</code> · {formatBytes(a.size_bytes)} ·{" "}
                <a href={a.licence_url} target="_blank" rel="noreferrer">
                  {a.licence}
                </a>
              </li>
            ))}
          </ul>
          <button
            className="set-btn"
            onClick={() => install.mutate()}
            disabled={install.isPending}
          >
            {install.isPending ? "Starting…" : "Download the face models"}
          </button>
        </>
      )}

      {running && job && (
        <>
          <p className="set-row__sub">
            {job.stage === "verifying"
              ? "Checking what arrived…"
              : job.stage === "installing"
                ? "Putting it in place…"
                : `Downloading ${job.asset ?? ""}`}{" "}
            — {pct}%
          </p>
          <button className="set-btn" onClick={() => cancel.mutate()}>
            Cancel
          </button>
        </>
      )}

      {job?.error && !running && (
        <p className="set-row__sub set-row__sub--warn">
          The download did not finish: {job.error}
        </p>
      )}

      {models?.installed && (
        <p className="set-row__sub">
          The models are installed in <code>{models.directory}</code>.
          {caps?.ready ? (
            " Face grouping is ready — open a picture library and press People."
          ) : (
            <>
              {/*
                Two different faults, and they had one sentence between them.

                A worker that never ran and a worker that ran and could not
                start its runtime are not the same problem, and "run the
                installer" is the fix for exactly one of them. Told the wrong
                one, somebody reinstalls LANcast to repair a library path — and
                when that does not help, the next thing they doubt is the
                download that was never at fault.

                `version` is the tell: the worker answers with its own version,
                so anything in that field means the binary is present and ran.
              */}
              {caps?.version ? (
                <>
                  {" "}
                  The worker is installed, but it could not start:{" "}
                  <strong>{caps.reason ?? "no reason given"}</strong>
                </>
              ) : (
                <>
                  {" "}
                  The worker itself is still missing:{" "}
                  <strong>{caps?.reason ?? "not installed"}</strong>. It ships
                  with the LANcast installer rather than the in-app update, so
                  running the installer for this version will supply it.
                </>
              )}
            </>
          )}
        </p>
      )}

      <SemanticSearchRow />

      {models?.supported &&
        !models.installed &&
        !running &&
        caps &&
        !caps.ready && (
          <p className="set-row__sub">
            {/*
            Said here because it is the single most likely reason this feature
            appears to do nothing: the in-app updater replaces the server only,
            and the worker arrives with the installer.
          */}
            Note: the face worker ships with the LANcast installer. If you
            updated from inside the app, run the installer for this version as
            well.
          </p>
        )}
    </section>
  );
}

/*
 * Searching photographs by description (ADR 0060).
 *
 * Its own component inside the Pictures pane rather than more branches in
 * PicturesSection, because it is a *second* optional download with its own
 * three states and its own progress. Interleaving them would produce a pane
 * where two independent installs share one progress sentence, and the first
 * time both are running nobody could tell which number belonged to which.
 *
 * The sizes are stated plainly. Six hundred megabytes is a real ask on a
 * metered connection, and a consent screen that rounds it away is not consent.
 */
function SemanticSearchRow() {
  const { data: caps } = useSemanticCapabilities();
  const { data: models } = useSemanticModels();
  const install = useInstallSemanticModels();
  const cancel = useCancelSemanticModels();

  const job = models?.job;
  const running = job?.running ?? false;
  const pct =
    job && job.bytes_total > 0
      ? Math.min(100, Math.round((job.bytes_done / job.bytes_total) * 100))
      : 0;

  return (
    <>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">
            Search photographs by description
          </div>
          <div className="set-row__sub">
            Find a photograph by describing what is in it — “a dog on a beach” —
            rather than by its filename. Like face grouping, it runs entirely on
            this machine: nothing is uploaded and no account is needed.
          </div>
        </div>
      </div>

      {models && !models.supported && (
        <p className="set-row__sub">
          {models.reason ?? "There is no download for this platform yet."}
        </p>
      )}

      {models?.supported && !models.installed && !running && (
        <>
          <p className="set-row__sub">
            This needs a one-off download of{" "}
            <strong>{formatBytes(models.bytes_total ?? 0)}</strong> — the model
            that reads photographs, the one that reads your words, and the
            vocabulary they share. Nothing is fetched until you press the
            button.
          </p>
          <ul className="set-assets">
            {(models.assets ?? []).map((a) => (
              <li key={a.name}>
                <code>{a.name}</code> · {formatBytes(a.size_bytes)} ·{" "}
                <a href={a.licence_url} target="_blank" rel="noreferrer">
                  {a.licence}
                </a>
                {/*
                  The shared runtime is listed even when it is already here, and
                  said to be already here. Dropping it would leave a list whose
                  sizes do not add up to the total above it, on the one screen
                  whose purpose is being trustworthy about numbers.
                */}
                {a.present ? " · already downloaded" : ""}
              </li>
            ))}
          </ul>
          <button
            className="set-btn"
            onClick={() => install.mutate()}
            disabled={install.isPending}
          >
            {install.isPending ? "Starting…" : "Download the search models"}
          </button>
        </>
      )}

      {running && job && (
        <>
          <p className="set-row__sub">
            {job.stage === "verifying"
              ? "Checking what arrived…"
              : job.stage === "installing"
                ? "Putting it in place…"
                : `Downloading ${job.asset ?? ""}`}{" "}
            — {pct}%
          </p>
          <button className="set-btn" onClick={() => cancel.mutate()}>
            Cancel
          </button>
        </>
      )}

      {job?.error && !running && (
        <p className="set-row__sub set-row__sub--warn">
          The download did not finish: {job.error}
        </p>
      )}

      {models?.installed && (
        <p className="set-row__sub">
          {caps?.semantic_ready ? (
            <>
              Ready — open a picture library and press <strong>Describe</strong>
              . Each library has to be indexed once before it can be searched.
            </>
          ) : (
            <>
              The models are installed, but the worker could not use them:{" "}
              <strong>{caps?.semantic_reason ?? "no reason given"}</strong>
            </>
          )}
        </p>
      )}
    </>
  );
}
