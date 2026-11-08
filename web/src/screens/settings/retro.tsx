import { formatBytes } from "@/lib/format";
import {
  useSettings,
  useUpdateSettings,
  useRetroDatabase,
  useWebCores,
  useInstallWebCores,
  useCancelWebCores,
  useInstallRetroDatabase,
  useCancelRetroDatabase,
} from "@/api/hooks";

/*
 * Server identity. /api/health has returned the version since the beginning and
 * nothing ever asked for it, so a settings page could not tell you which server
 * it was configuring — the first question anyone has when something behaves
 * unexpectedly after an update.
 */
/*
 * Retro games (ADR 0073): the ROM database and box art.
 *
 * Two separate consents, deliberately apart. The database is a one-off
 * download after which naming games is offline; box art is a fetch per game,
 * every time a game is identified. Somebody may well want the first and not
 * the second, and one switch for both would make that impossible.
 */
export function RetroSection() {
  const { data: settings } = useSettings();
  const update = useUpdateSettings();
  const { data: db } = useRetroDatabase();
  const install = useInstallRetroDatabase();
  const cancel = useCancelRetroDatabase();

  const job = db?.job;
  const running = job?.running ?? false;
  const pct =
    job && job.bytes_total > 0
      ? Math.min(100, Math.round((job.bytes_done / job.bytes_total) * 100))
      : 0;
  const ident = db?.identify;

  return (
    <section className="settings__section">
      <span className="section-label">Retro games</span>

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Name games from the ROM database</div>
          <div className="set-row__sub">
            Each game is identified by what is inside the file, not by its name,
            so a renamed or badly named ROM still gets its proper title and
            year. It runs entirely on this machine once the database is here.
          </div>
        </div>
      </div>

      {db && !db.installed && !running && (
        <>
          <p className="set-row__sub">
            This needs a one-off download of{" "}
            <strong>{formatBytes(db.bytes_total)}</strong> —{" "}
            {db.files.length} files from libretro-database, licensed{" "}
            <a href={db.licence_url} target="_blank" rel="noreferrer">
              {db.licence}
            </a>
            . Nothing is fetched until you press the button. Without it, games
            keep the titles their file names give them.
          </p>
          <button
            className="set-btn"
            onClick={() => install.mutate()}
            disabled={install.isPending}
          >
            {install.isPending ? "Starting…" : "Download the ROM database"}
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
                : `Downloading ${job.file ?? ""}`}{" "}
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

      {db?.installed && (
        <p className="set-row__sub">
          The ROM database is installed.
          {ident?.running
            ? ` Identifying games — ${ident.matched + ident.unmatched + ident.failed} of ${ident.total}.`
            : ident && ident.matched + ident.unmatched > 0
              ? ` Last pass: ${ident.matched} named, ${ident.unmatched} not in the database.`
              : ""}
        </p>
      )}

      <WebCoresBlock />

      {settings && (
        <>
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.retro_artwork}
              onChange={(e) =>
                update.mutate({ retro_artwork: e.target.checked })
              }
            />
            Fetch box art for identified games
          </label>
          <p className="set-row__sub">
            Box art and a screenshot for each game come from
            libretro-thumbnails, one download per game. Off by default because
            it reaches the internet; turning it on fetches art for games already
            identified.
          </p>
        </>
      )}
    </section>
  );
}

/*
 * The browser's emulator cores (ADR 0077).
 *
 * What would be fetched, from where, how big and under which licences, all
 * said before the button, because a download somebody cannot identify is not
 * consent. The licences are listed per core rather than summarised: they are
 * not all the same, and two of them are the GPL.
 */
function WebCoresBlock() {
  const { data } = useWebCores();
  const install = useInstallWebCores();
  const cancel = useCancelWebCores();
  if (!data) return null;

  const job = data.job;
  const running = job.running;
  const missing = data.cores.filter((c) => !c.installed);
  const pct = job.total > 0 ? Math.min(100, Math.round((job.done / job.total) * 100)) : 0;

  return (
    <>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Play in a browser</div>
          <div className="set-row__sub">
            Lets a browser or a television play NES, Game Boy, Game Boy Color,
            Game Boy Advance, Master System and Mega Drive games without the
            desktop app. SNES, N64 and PlayStation stay in the desktop app.
          </div>
        </div>
      </div>

      {missing.length > 0 && !running && (
        <>
          <p className="set-row__sub">
            This server downloads the emulator cores from libretro once:{" "}
            <strong>{formatBytes(data.archive.size_bytes)}</strong>, RetroArch{" "}
            {data.archive.version}, checked against pinned checksums. About{" "}
            {formatBytes(missing.reduce((n, c) => n + c.size_bytes, 0))} is kept and
            the rest deleted. LANcast does not ship them; they are only served to
            people signed in here. Nothing is fetched until you press the button.
          </p>
          <ul className="set-row__sub">
            {data.cores.map((c) => (
              <li key={c.name}>
                {c.display} ({c.licence},{" "}
                <a href={c.source_url} target="_blank" rel="noreferrer">
                  source
                </a>
                ){c.installed ? " — installed" : ""}
              </li>
            ))}
          </ul>
          <button className="set-btn" onClick={() => install.mutate()} disabled={install.isPending}>
            {install.isPending ? "Starting…" : "Download the browser cores"}
          </button>
        </>
      )}

      {running && (
        <>
          <p className="set-row__sub">
            {job.stage === "unpack"
              ? `Taking the cores out — ${job.done} of ${job.total}`
              : `Downloading — ${pct}%`}
          </p>
          <button className="set-btn" onClick={() => cancel.mutate()}>
            Cancel
          </button>
        </>
      )}

      {job.error && !running && (
        <p className="set-row__sub set-row__sub--warn">
          The download did not finish: {job.error}
        </p>
      )}

      {missing.length === 0 && (
        <p className="set-row__sub">
          The browser cores are installed (RetroArch {data.archive.version}).
        </p>
      )}
    </>
  );
}
