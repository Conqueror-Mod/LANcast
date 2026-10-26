import { useEffect, useState } from "react";
import { useBackups, useTakeBackup, useDeleteBackup, useRestoreBackup } from "@/api/hooks";
import type { BackupFile, RestoreOutcome } from "@/api/types";

/*
 * Backups, in Settings (ADR 0058).
 *
 * The screen has one job beyond the button: to be honest about what a backup
 * is. It is the database — every correction, rating, watch position, playlist
 * and lock, which is the part a rescan can never rebuild — and it is *not* the
 * artwork or the media. Somebody who believes this file contains their films
 * has not been given a backup, they have been given a false sense of one.
 *
 * Restore is staged, not done in place. Restoring replaces the database the
 * server is reading, so the button asks the server to take a backup of what is
 * there now, stage the one chosen, and restart; the next start swaps the file
 * while nothing holds it. The command is still shown, because it is the way
 * back when the server will not start at all.
 *
 * No gold anywhere here. Gold means *where you are* and nothing else
 * (docs/design.md); the moment it also meant "this backup is good", the focus
 * signal would be dead.
 */

// humanBytes is how a person tells two backups apart at a glance.
export function humanBytes(n: number): string {
  if (n < 1024) return `${n} bytes`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = n / 1024;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value.toFixed(1)} ${units[i]}`;
}

/*
 * takenAt formats the timestamp in *local* time.
 *
 * Not a stylistic choice. A date built from UTC components reads as tomorrow's
 * backup all evening in every US timezone, and that mistake has shipped in this
 * project before. `toLocaleString` on a Date built from epoch seconds is local
 * by construction, which is why it is used rather than slicing an ISO string.
 */
export function takenAt(epochSeconds: number): string {
  return new Date(epochSeconds * 1000).toLocaleString();
}

/*
 * What one look at the server says about the restart: not answering, still the
 * session that asked for the restore, or answering with that session gone.
 */
export type ServerState = "down" | "same" | "restarted";

/*
 * waitForRestart polls until the server is back with the restored database,
 * then reloads.
 *
 * "Answers" alone is not enough: the first poll can land before the server has
 * stopped, and reloading then would show the old database for a moment. Two
 * things count as back. Seeing the server not answer and then answer again;
 * and the server answering with this session gone, which only the restore
 * does — the backup holds no sessions. The second exists because the first
 * can be missed: a restart quick enough that every poll got an answer left
 * this waiting for its whole fallback while the app, finding itself signed
 * out, put the sign-in page up over the old cache (2026-10-26, a library still
 * wearing the name the restore had taken away). After a minute and a half it
 * reloads anyway.
 */
export async function waitForRestart(
  look: () => Promise<ServerState>,
  reload: () => void,
  wait: (ms: number) => Promise<void> = (ms) => new Promise((r) => setTimeout(r, ms)),
  limitMs = 90_000,
) {
  const started = Date.now();
  let wentAway = false;
  while (Date.now() - started < limitMs) {
    const state = await look();
    if (state === "restarted") break;
    if (state === "down") wentAway = true;
    else if (wentAway) break;
    await wait(1000);
  }
  reload();
}

async function lookAtServer(): Promise<ServerState> {
  try {
    const health = await fetch("/api/health", { cache: "no-store" });
    if (!health.ok) return "down";
    const status = await fetch("/api/auth/status", { cache: "no-store" });
    if (!status.ok) return "down";
    const body = (await status.json()) as { authenticated?: boolean };
    // Only an explicit "not signed in" counts. An answer without the field is
    // not this server's auth status and proves nothing either way.
    return body.authenticated === false ? "restarted" : "same";
  } catch {
    return "down";
  }
}

export function BackupSettings() {
  const { data, isLoading, error } = useBackups(true);
  const take = useTakeBackup();
  const remove = useDeleteBackup();
  const restore = useRestoreBackup();
  // Which backup is one click from being deleted. A name rather than a
  // boolean, so arming one row cannot arm every row — the same list can hold a
  // dozen of these and they are all called nearly the same thing.
  const [confirming, setConfirming] = useState<string | null>(null);
  // Which backup is asking "restore this one?", the same way.
  const [restoreArmed, setRestoreArmed] = useState<string | null>(null);
  // The backup being restored, once the server has said yes.
  const [restoring, setRestoring] = useState<string | null>(null);

  useEffect(() => {
    if (!restoring) return;
    void waitForRestart(lookAtServer, () => window.location.reload());
  }, [restoring]);

  const backups = data?.backups ?? [];

  if (restoring) {
    return (
      <section className="settings__section">
        <span className="section-label">Backup</span>
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title" role="status">
              Restarting to restore {restoring}&hellip;
            </div>
            <div className="set-row__sub">
              The page reloads when the server is back. Everyone is signed out, so
              you&rsquo;ll sign in again.
            </div>
          </div>
        </div>
      </section>
    );
  }

  return (
    <section className="settings__section">
      <span className="section-label">Backup</span>

      {data?.last_restore && <LastRestore outcome={data.last_restore} />}
      {data?.staged && (
        <div className="set-row__sub set-row__sub--standalone" role="status">
          A restore of {data.staged.name} is waiting for the server to restart.
        </div>
      )}

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Take a backup</div>
          <div className="set-row__sub">
            Copies the database: every correction you have made by hand, watch
            history and positions, ratings, playlists, collections and locked
            fields. A rescan can rebuild what was read from your files; it
            deliberately cannot rebuild any of this.
            <br />
            <strong>Your media and artwork are not included.</strong> Posters
            are downloaded again after a restore; films and music are on your
            drives, where a backup of them would not fit. Nobody is signed out,
            and a backup holds no logins.
          </div>
        </div>
        <div className="set-row__actions">
          <button
            className="set-btn"
            disabled={take.isPending}
            onClick={() => take.mutate()}
          >
            {take.isPending ? "Taking…" : "Take a backup"}
          </button>
        </div>
      </div>

      {take.error && (
        <div className="set-row__sub set-row__sub--standalone">
          Could not take a backup: {take.error.message}
        </div>
      )}

      {error && (
        <div className="set-row__sub set-row__sub--standalone">
          Could not read the backup folder: {error.message}
        </div>
      )}

      {!isLoading && backups.length === 0 && !error && (
        <div className="set-row__sub set-row__sub--standalone">
          No backups yet. Nothing here is protected until one is taken — by
          the schedule below, or by you now.
        </div>
      )}

      {backups.map((b) => (
        <div key={b.name}>
          <BackupRow
            backup={b}
            armed={confirming === b.name}
            busy={remove.isPending || restore.isPending}
            canRestore={!!data?.can_restore && b.restorable && !data?.staged}
            onArm={() => setConfirming(b.name)}
            onDelete={() =>
              remove.mutate(b.name, { onSuccess: () => setConfirming(null) })
            }
            onRestore={() => {
              setConfirming(null);
              setRestoreArmed(b.name);
            }}
          />
          {restoreArmed === b.name && (
            // In the page rather than confirm(): a native dialog in the LANcast
            // window loses keyboard focus when it closes.
            <div className="set-row" role="alertdialog" aria-label="Restore this backup">
              <div className="set-row__main">
                <div className="set-row__title">Restore the backup from {takenAt(b.taken_at)}?</div>
                <div className="set-row__sub">
                  LANcast first backs up the database as it is now, so this can be
                  undone from this list. Then the server restarts and puts this
                  backup in its place. Anyone watching is interrupted, everyone is
                  signed out, and anything changed since this backup was taken is
                  replaced. Posters download again over the next while.
                </div>
                <div className="set-row">
                  <button
                    className="set-btn"
                    type="button"
                    disabled={restore.isPending}
                    onClick={() =>
                      restore.mutate(b.name, {
                        onSuccess: () => {
                          setRestoreArmed(null);
                          setRestoring(b.name);
                        },
                      })
                    }
                  >
                    {restore.isPending ? "Backing up first…" : "Restore and restart"}
                  </button>
                  <button className="set-btn" type="button" onClick={() => setRestoreArmed(null)}>
                    Cancel
                  </button>
                </div>
                {restore.error && <p className="set-error">{restore.error.message}</p>}
              </div>
            </div>
          )}
        </div>
      ))}

      {data && (
        <>
          <div className="set-row">
            <div className="set-row__main">
              <div className="set-row__title">Where they are kept</div>
              <div className="set-row__sub">
                <code>{data.folder}</code>
                <br />
                These sit on the same drive as the library they protect, so a
                failed disk takes both. Download one, or copy it somewhere else,
                and it becomes a backup.
              </div>
            </div>
          </div>

          <div className="set-row">
            <div className="set-row__main">
              <div className="set-row__title">Restoring</div>
              <div className="set-row__sub">
                {data.can_restore
                  ? "Restore on a backup above puts it back: the server backs up what you have now, restarts, and swaps the database while nothing is using it. "
                  : "This server can't restart itself, so restoring is done on the machine with the server stopped. "}
                If the server won&rsquo;t start at all, the same thing can be done by
                hand with the server stopped:
                <br />
                <code>{data.restore_command}</code>
                <br />
                Either way, the database it replaces is also kept beside it.
              </div>
            </div>
          </div>
        </>
      )}
    </section>
  );
}

/*
 * LastRestore says what the last restore from here did, after the restart that
 * carried it out, when the request that asked for it is long gone. A failure
 * is the important case: the server started anyway, with the database it had,
 * and without this nobody would know the restore never happened.
 */
function LastRestore({ outcome }: { outcome: RestoreOutcome }) {
  const when = takenAt(outcome.applied_at);
  if (!outcome.ok) {
    return (
      <p className="set-error" role="alert">
        The restore of {outcome.name} on {when} didn&rsquo;t happen: {outcome.error}. The
        server started with the database it already had.
      </p>
    );
  }
  return (
    <div className="set-row__sub set-row__sub--standalone">
      Restored {outcome.name} on {when}
      {outcome.requested_by ? `, at ${outcome.requested_by}'s request` : ""}.
      {outcome.safety_backup &&
        ` The database from before it is the backup ${outcome.safety_backup}.`}
    </div>
  );
}

function BackupRow({
  backup,
  armed,
  busy,
  canRestore,
  onArm,
  onDelete,
  onRestore,
}: {
  backup: BackupFile;
  armed: boolean;
  busy: boolean;
  canRestore: boolean;
  onArm: () => void;
  onDelete: () => void;
  onRestore: () => void;
}) {
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{takenAt(backup.taken_at)}</div>
        <div className="set-row__sub">
          {backup.pre_migration
            ? `Before an update (database version ${backup.schema_version}) · `
            : backup.scheduled
              ? "Scheduled · "
              : "Taken by hand · "}
          {humanBytes(backup.bytes)} · {backup.name}
          {/* A backup this build cannot restore is the single most important
              thing this list can say, so it is said on the row rather than
              left to be discovered during a restore. */}
          {!backup.restorable && (
            <>
              <br />
              <strong>Cannot be restored: {backup.problem}</strong>
            </>
          )}
        </div>
      </div>
      <div className="set-row__actions">
        {/* A plain link, so the browser streams it to disk rather than the
            application holding a hundred megabytes in memory to hand over. */}
        <a
          className="set-btn"
          href={`/api/backups/${encodeURIComponent(backup.name)}`}
          download={backup.name}
        >
          Download
        </a>
        {canRestore && (
          <button className="set-btn" disabled={busy} onClick={onRestore}>
            Restore
          </button>
        )}
        <button
          className="set-btn"
          disabled={busy}
          onClick={armed ? onDelete : onArm}
        >
          {armed ? "Delete for good?" : "Delete"}
        </button>
      </div>
    </div>
  );
}
