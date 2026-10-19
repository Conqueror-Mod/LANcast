import { useRef, useState } from "react";
import { ApiFailure } from "@/api/client";
import { useDeleteMilkdropPreset, useMilkdropPresets, useUploadMilkdropPreset } from "@/api/hooks";
import type { MilkdropPresetSummary } from "@/api/types";
import { humanBytes } from "./BackupSettings";

/*
 * MilkDrop presets (ADR 0091), for an administrator: upload, list, remove.
 *
 * Administrators only, as the server enforces: a preset is something every
 * signed-in person's browser will run, so what is on the server is an
 * administrator's choice. LANcast ships none; these are presets the household
 * has, on the same footing as its own media.
 *
 * Every failure is said in words a person can act on. The server refuses a
 * preset its parser cannot read with the place and the reason, and that is
 * passed through, prefixed with which file it was.
 */

/** The server's limit, checked here too so a wrong file is not sent at all. */
export const MILKDROP_MAX_BYTES = 512 * 1024;

const ACCEPT = [".milk", ".json"];

/** What to tell somebody whose file was refused, before or after sending it. */
export function uploadError(name: string, err: unknown): string {
  const file = `“${name}”`;
  if (err instanceof ApiFailure) {
    if (err.status === 413 || err.code === "too_large")
      return `${file} is larger than 512 KB, which is far more than a preset needs. Is it the right file?`;
    if (err.code === "invalid_preset") return `${file} could not be used. ${sentence(err.message)}`;
    if (err.status === 403) return "Only an administrator can add or remove presets.";
    if (err.status === 401) return "You have been signed out. Sign in again, then try once more.";
    return `${file} could not be uploaded. ${sentence(err.message)}`;
  }
  if (err instanceof TypeError) return `${file} was not sent: the server could not be reached.`;
  return `${file} could not be uploaded. ${sentence(String((err as Error)?.message ?? err))}`;
}

/** A refusal before anything is sent, or null when the file may go. */
export function checkFile(name: string, size: number): string | null {
  const lower = name.toLowerCase();
  if (!ACCEPT.some((ext) => lower.endsWith(ext)))
    return `“${name}” is not a MilkDrop preset. Choose a .milk file, or butterchurn's .json.`;
  if (size > MILKDROP_MAX_BYTES)
    return `“${name}” is larger than 512 KB, which is far more than a preset needs. Is it the right file?`;
  if (size === 0) return `“${name}” is empty.`;
  return null;
}

// The server's own words, ended as a sentence. Not capitalised: they often
// begin with a block's name, and `per_frame_12` is what the file says.
function sentence(s: string): string {
  const t = s.trim();
  if (!t) return "";
  return /[.!?]$/.test(t) ? t : t + ".";
}

function whenUploaded(unix: number): string {
  return new Date(unix * 1000).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

export function MilkdropSettings() {
  const { data: presets, isLoading, error } = useMilkdropPresets();
  const upload = useUploadMilkdropPreset();
  const fileRef = useRef<HTMLInputElement>(null);
  const [problems, setProblems] = useState<string[]>([]);
  const [notes, setNotes] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const onFiles = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? []);
    e.target.value = ""; // the same file can be chosen again
    if (files.length === 0) return;
    setBusy(true);
    const bad: string[] = [];
    const said: string[] = [];
    // One at a time: each is parsed on the server, and one refusal must not
    // stop the rest.
    for (const f of files) {
      const refused = checkFile(f.name, f.size);
      if (refused) {
        bad.push(refused);
        continue;
      }
      try {
        const got = await upload.mutateAsync({ name: f.name, data: await f.arrayBuffer() });
        // The id is the preset's hash, so an id already listed is the same
        // preset, whatever the file was called.
        if ((presets ?? []).some((p) => p.id === got.id))
          said.push(`“${f.name}” was already here, as “${got.name}”.`);
      } catch (err) {
        bad.push(uploadError(f.name, err));
      }
    }
    setProblems(bad);
    setNotes(said);
    setBusy(false);
  };

  return (
    <section className="settings__section">
      <span className="section-label">MilkDrop presets</span>
      <p className="set-row__sub">
        Presets for the MilkDrop visualizer in Now Playing, for everyone on this server. Upload a{" "}
        <code>.milk</code> file or butterchurn&apos;s <code>.json</code>. A preset&apos;s equations are
        checked here and never run as code, and every preset is drawn through LANcast&apos;s own pass,
        which holds flashing to three a second and keeps gold for focus.
      </p>

      {isLoading && <p className="set-row__sub">Loading…</p>}
      {error && <p className="set-error">The presets could not be listed: {(error as Error).message}</p>}
      {presets && presets.length === 0 && (
        <p className="set-row__sub">No presets yet. MilkDrop appears under Now Playing once there is one.</p>
      )}
      {presets && presets.length > 0 && (
        <div className="set-lib" role="list" aria-label="MilkDrop presets">
          {presets.map((p) => (
            <PresetRow key={p.id} preset={p} />
          ))}
        </div>
      )}

      <input
        ref={fileRef}
        type="file"
        accept={ACCEPT.join(",")}
        multiple
        onChange={onFiles}
        style={{ display: "none" }}
        aria-label="Preset files"
      />
      <div className="set-row__inline">
        <button className="set-btn" onClick={() => fileRef.current?.click()} disabled={busy}>
          {busy ? "Checking…" : "Upload presets…"}
        </button>
      </div>
      {problems.map((m) => (
        <p key={m} className="set-error" role="alert">
          {m}
        </p>
      ))}
      {notes.map((m) => (
        <p key={m} className="set-row__sub">
          {m}
        </p>
      ))}
    </section>
  );
}

function PresetRow({ preset }: { preset: MilkdropPresetSummary }) {
  const remove = useDeleteMilkdropPreset();
  const [confirming, setConfirming] = useState(false);
  const facts = [
    preset.source === "milk" ? ".milk" : "butterchurn JSON",
    humanBytes(preset.size_bytes),
    `added ${whenUploaded(preset.uploaded_at)}`,
  ];
  return (
    <div className="set-row" role="listitem">
      <div className="set-row__main">
        <div className="set-row__title">{preset.name}</div>
        <div className="set-row__sub">{facts.join(" · ")}</div>
        {preset.shaders === "not_converted" && (
          <div className="set-row__sub">
            Shaders not converted: it plays with MilkDrop&apos;s default shaders, so it may not look as
            its author meant.
          </div>
        )}
        {remove.isError && (
          <div className="set-error">
            “{preset.name}” could not be removed. {sentence((remove.error as Error).message)}
          </div>
        )}
      </div>
      <div className="set-row__actions">
        {confirming ? (
          <>
            <span className="set-confirm">Remove?</span>
            <button
              className="set-btn set-btn--danger"
              onClick={() => remove.mutate(preset.id, { onSettled: () => setConfirming(false) })}
              disabled={remove.isPending}
            >
              Yes
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              No
            </button>
          </>
        ) : (
          <button
            className="set-btn set-btn--danger"
            onClick={() => setConfirming(true)}
            aria-label={`Remove ${preset.name}`}
          >
            Remove
          </button>
        )}
      </div>
    </div>
  );
}
