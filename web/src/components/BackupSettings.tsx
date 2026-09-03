import { useState } from "react";
import { useBackups, useTakeBackup, useDeleteBackup } from "@/api/hooks";
import type { BackupFile } from "@/api/types";

/*
 * Backups, in Settings (ADR 0058).
 *
 * The screen has one job beyond the button: to be honest about what a backup
 * is. It is the database — every correction, rating, watch position, playlist
 * and lock, which is the part a rescan can never rebuild — and it is *not* the
 * artwork or the media. Somebody who believes this file contains their films
 * has not been given a backup, they have been given a false sense of one.
 *
 * There is no restore button, and that is the design rather than a gap.
 * Restoring replaces the database the server is reading, so it happens with
 * the server stopped. The command is shown instead of a control that would
 * have to lie about what it can do.
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

export function BackupSettings() {
  const { data, isLoading, error } = useBackups(true);
  const take = useTakeBackup();
  const remove = useDeleteBackup();
  // Which backup is one click from being deleted. A name rather than a
  // boolean, so arming one row cannot arm every row — the same list can hold a
  // dozen of these and they are all called nearly the same thing.
  const [confirming, setConfirming] = useState<string | null>(null);

  const backups = data?.backups ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">Backup</span>

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
          No backups yet. Nothing here is protected until you take one.
        </div>
      )}

      {backups.map((b) => (
        <BackupRow
          key={b.name}
          backup={b}
          armed={confirming === b.name}
          busy={remove.isPending}
          onArm={() => setConfirming(b.name)}
          onDelete={() =>
            remove.mutate(b.name, { onSuccess: () => setConfirming(null) })
          }
        />
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
                Restoring happens with the server stopped, so it is done on the
                machine rather than from here — swapping the database out from
                under a running server is how a restore becomes the thing it was
                meant to prevent.
                <br />
                <code>{data.restore_command}</code>
                <br />
                The database it replaces is kept beside it, so restoring the
                wrong one can be undone.
              </div>
            </div>
          </div>
        </>
      )}
    </section>
  );
}

function BackupRow({
  backup,
  armed,
  busy,
  onArm,
  onDelete,
}: {
  backup: BackupFile;
  armed: boolean;
  busy: boolean;
  onArm: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{takenAt(backup.taken_at)}</div>
        <div className="set-row__sub">
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
