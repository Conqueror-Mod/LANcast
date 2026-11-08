import { useState } from "react";
import {
  useSettings,
  useUpdateSettings,
  useProbeStatus,
  useReprobe,
  useMediaTools,
  useInstallMediaTools,
  useCancelMediaToolsInstall,
} from "@/api/hooks";
import { errorMessage } from "@/lib/errors";
import type { Settings as SettingsType, SettingsUpdate } from "@/api/types";
import { RuleNumber } from "./shared";

// ffmpeg/ffprobe status. This is a row rather than a hidden detail because its
// absence is otherwise invisible: nothing gets probed, every file is
// direct-played, and whatever the browser cannot decode fails with no
// explanation — which is exactly how a whole library went unplayable unnoticed.
function MediaToolsRow({
  settings,
  update,
}: {
  settings: SettingsType;
  update: { mutate: (u: SettingsUpdate) => void; isPending: boolean };
}) {
  const tools = settings.media_tools;
  const ok = tools?.probe_available;
  const [value, setValue] = useState("");
  /*
   * The install job, polled only while it matters.
   *
   * Enabled when the tools are missing or a job is running: once ffmpeg is
   * present there is nothing to watch, and a settings page that polls forever
   * for a finished download is a page that costs a request a second for nothing.
   */
  const job = useMediaTools(!ok);
  const install = useInstallMediaTools();
  const cancel = useCancelMediaToolsInstall();
  const state = job.data;
  const src = state?.available_source;
  const running = !!state?.running;
  const pct =
    state && state.bytes_total > 0
      ? Math.min(100, Math.round((state.bytes_done / state.bytes_total) * 100))
      : 0;
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">
          Media tools{" "}
          <span
            className={
              "addon-signer addon-signer--" + (ok ? "first_party" : "unsigned")
            }
          >
            {ok ? "found" : "missing"}
          </span>
        </div>
        <div className="set-row__sub">
          {ok
            ? `ffmpeg and ffprobe available${tools.directory ? " · " + tools.directory : " on PATH"}`
            : "Without ffmpeg, LANcast cannot inspect or convert media — files play only if your browser already supports them. Download it below, or set the folder containing it here."}
        </div>

        {/* What is about to be downloaded, before it is: version, size and
            licence. A download the user cannot identify is not consent, and the
            licence is GPL, which is worth saying out loud rather than burying in
            an ADR. */}
        {!ok && src && !running && (
          <div className="set-row__sub">
            {`${(src.size_bytes / 1_000_000).toFixed(0)} MB · ffmpeg ${src.version} · `}
            <a href={src.licence_url} target="_blank" rel="noreferrer">
              {src.licence}
            </a>
            {" · fetched from GitHub, checksum verified before it is unpacked"}
          </div>
        )}

        {running && (
          <div className="set-row__sub" role="status">
            {state?.stage === "downloading"
              ? `Downloading… ${pct}% of ${(state.bytes_total / 1_000_000).toFixed(0)} MB`
              : state?.stage === "verifying"
                ? "Verifying the checksum…"
                : "Installing…"}
          </div>
        )}

        {/* A failed install says why and stays failed until something is done
            about it, rather than reverting to a bare "missing" that looks like
            nothing was ever attempted. */}
        {!running && state?.error && (
          <div className="set-row__sub" role="alert">
            {`That install did not finish: ${state.error}`}
          </div>
        )}
      </div>
      <div className="set-row__actions">
        {/* Downloading it is offered first, because typing a path presupposes
            having already installed ffmpeg somewhere — the step this exists to
            remove. The folder box stays for anyone who has. */}
        {!ok && src && !running && (
          <button
            className="set-btn"
            disabled={install.isPending}
            onClick={() => install.mutate()}
          >
            {install.isPending ? "Starting…" : "Download ffmpeg"}
          </button>
        )}
        {running && (
          <button className="set-btn" onClick={() => cancel.mutate()}>
            Cancel
          </button>
        )}
        <input
          className="set-input set-input--wide"
          type="text"
          placeholder={tools?.directory || "Folder containing ffmpeg"}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <button
          className="set-btn"
          disabled={update.isPending || !value.trim()}
          onClick={() => {
            update.mutate({ ffmpeg_dir: value.trim() });
            setValue("");
          }}
        >
          Save
        </button>
      </div>
    </div>
  );
}

// Re-probing. Sits under the media-tools row because it is the same subject:
// what the server knows about your files, and what to do when that knowledge
// is out of date.
//
// Exists because a probe is only as good as the build that made it. Nothing
// revisits an already-probed file on its own, so when the prober learns to read
// something new — bit depth being the case that prompted this — every older
// file keeps a playback decision made without it, permanently.
//
// The full re-probe asks for confirmation inline rather than in a dialog. It is
// not destructive — nothing is deleted, and playback keeps working throughout —
// so a modal would overstate it, but it is still real work across every file
// and more than a single stray click should start.
//
// Measured: 225 files on local storage took ~15s. The cost scales with file
// count and storage speed, not library size in bytes, so the warning says "a
// few minutes" rather than naming a number this component cannot know.
function ReprobeRow({ available }: { available: boolean }) {
  const status = useProbeStatus(available);
  const reprobe = useReprobe();
  const [confirming, setConfirming] = useState(false);
  const [queued, setQueued] = useState<number | null>(null);

  const running = status.data?.running ?? false;
  const busy = reprobe.isPending || running;

  function run(scope: "incomplete" | "all") {
    setConfirming(false);
    reprobe.mutate(scope, {
      onSuccess: (r) => setQueued(r.queued),
    });
  }

  let sub: string;
  if (!available) {
    sub = "Install ffmpeg to inspect your files.";
  } else if (running) {
    const { probed = 0, total = 0 } = status.data ?? {};
    sub =
      total > 0 ? `Reading files — ${probed} of ${total}.` : "Reading files…";
  } else if (reprobe.isError) {
    sub = (reprobe.error as Error).message;
  } else if (queued === 0) {
    sub = "Nothing to re-read — every file is already up to date.";
  } else if (queued !== null) {
    sub = `Queued ${queued} file${queued === 1 ? "" : "s"}.`;
  } else {
    // .set-row__sub is a single ellipsised line (nowrap), so this has to stay
    // short enough to read in full. The title carries the "what"; this carries
    // the only thing not obvious from it — when you would want to.
    sub = "Worth doing after an upgrade.";
  }

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Re-read media files</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        {confirming ? (
          <>
            <span className="set-confirm">
              Runs on every file — a few minutes for most libraries.
            </span>
            <button className="set-btn" onClick={() => run("all")}>
              Re-read everything
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              Cancel
            </button>
          </>
        ) : (
          <>
            <button
              className="set-btn"
              disabled={!available || busy}
              onClick={() => run("incomplete")}
            >
              Only what's missing
            </button>
            <button
              className="set-btn"
              disabled={!available || busy}
              onClick={() => setConfirming(true)}
            >
              Everything
            </button>
          </>
        )}
      </div>
    </div>
  );
}

/*
 * Where optimized copies live and how much space they may take (ADR 0087),
 * in Server › Transcoding beside the live conversion rules.
 */
function OptimizedStorageSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  const [dir, setDir] = useState<string | null>(null);
  if (!settings) return null;
  const limit = settings.optimized_limit_gb ?? 200;
  return (
    <section className="settings__section">
      <span className="section-label">Optimized versions</span>
      <label className="set-row">
        <span>Folder</span>
        <input
          className="set-input"
          placeholder="In LANcast's data folder"
          value={dir ?? settings.optimized_dir ?? ""}
          onChange={(e) => setDir(e.target.value)}
          onBlur={() => {
            if (dir !== null && dir.trim() !== (settings.optimized_dir ?? "")) {
              update.mutate({ optimized_dir: dir.trim() });
            }
            setDir(null);
          }}
        />
      </label>
      <p className="set-row__sub">
        A full path, such as <code>D:\LANcast\optimized</code>. Not inside a library, or a scan would
        take the copies for new films. Leave empty for LANcast&rsquo;s data folder.
      </p>
      <label className="set-row">
        <span>Space they may use</span>
        <select
          className="set-input"
          value={limit}
          onChange={(e) => update.mutate({ optimized_limit_gb: Number(e.target.value) })}
        >
          {[50, 100, 200, 500, 1000, 2000].concat([50, 100, 200, 500, 1000, 2000].includes(limit) ? [] : [limit])
            .map((gb) => (
              <option key={gb} value={gb}>
                {gb >= 1000 ? `${gb / 1000} TB` : `${gb} GB`}
              </option>
            ))}
        </select>
      </label>
      {update.isError && <span className="set-error">{errorMessage(update.error)}</span>}
    </section>
  );
}

export function TranscodingPane() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  return (
    <>
      <OptimizedStorageSection />
      <section className="settings__section">
        <span className="section-label">Transcoding</span>
        {settings && (
          <div className="set-row">
            <div className="set-row__main">
              <div className="set-row__title">Video encoder</div>
              <div className="set-row__sub">
                Active: {settings.encoder.active.label}
                {settings.encoder.active.hardware ? " (hardware)" : ""}
              </div>
            </div>
            <div className="set-row__actions">
              <select
                className="set-input"
                value={settings.encoder.preference}
                onChange={(e) =>
                  update.mutate({ hardware_encoder: e.target.value })
                }
              >
                <option value="auto">Auto</option>
                {settings.encoder.available.map((enc) => (
                  <option key={enc.name} value={enc.name}>
                    {enc.label}
                  </option>
                ))}
              </select>
            </div>
          </div>
        )}
        {settings && (
          <>
            {/*
              Beside the encoder because both are the same kind of decision:
              what this machine is willing to spend on delivering media, set
              by whoever runs it rather than by whoever is watching.
            */}
            <RuleNumber
              title="Conversions at once"
              sub="How many files this server will convert at the same time. Each one is a separate ffmpeg working flat out, so this is really a statement about the machine: past the limit the next person is told the server is busy, which is better than everybody's picture stuttering at once. Files that play as they are do not count — they are sent untouched, however many people are watching."
              value={settings.max_transcodes ?? 3}
              min={1}
              max={64}
              onCommit={(v) => update.mutate({ max_transcodes: v })}
            />

            <MediaToolsRow settings={settings} update={update} />
            <ReprobeRow available={!!settings.media_tools?.probe_available} />
          </>
        )}
      </section>
    </>
  );
}
