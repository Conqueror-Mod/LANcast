import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { apiGet, apiPost } from "@/api/client";
import { useBackHandler } from "@/focus/FocusController";
import { errorMessage } from "@/lib/errors";
import "./AddToPlaylist.css";
import "./ReportIssue.css";

/*
 * Report an issue (ADR 0109): written here, sent by the server to the
 * developer when, and only when, Send is pressed.
 *
 * It replaced a mailto link, which in the desktop window opened Windows'
 * "pick an app" box and then went nowhere, and which could never carry a
 * log. Nothing here leaves the app: no browser, no mail program.
 *
 * What goes with the report is said beside the button, and the log, which
 * only an administrator can attach, can be read in full before it is sent.
 *
 * Pictures — a screenshot, mostly — are chosen or pasted, shown before they
 * go, and can be taken off again. The server encodes each one afresh, which
 * also drops what a phone photo records about where it was taken.
 */

// The server's limits (internal/report): four pictures, each at most 6 MB as
// sent. Anything larger is shrunk here first.
const MAX_PICTURES = 4;
const MAX_PICTURE_BYTES = 6 << 20;

interface Picture {
  id: number;
  blob: Blob;
  url: string;
}

// A picture too large to send, drawn again smaller as JPEG; null where this
// browser cannot, or the file is not a picture it can open.
async function shrink(file: Blob): Promise<Blob | null> {
  if (typeof createImageBitmap !== "function") return null;
  try {
    const bmp = await createImageBitmap(file);
    for (const edge of [2560, 1920, 1280]) {
      const scale = Math.min(1, edge / Math.max(bmp.width, bmp.height));
      const canvas = document.createElement("canvas");
      canvas.width = Math.round(bmp.width * scale);
      canvas.height = Math.round(bmp.height * scale);
      const ctx = canvas.getContext("2d");
      if (!ctx) return null;
      ctx.drawImage(bmp, 0, 0, canvas.width, canvas.height);
      const out = await new Promise<Blob | null>((r) => canvas.toBlob(r, "image/jpeg", 0.85));
      if (out && out.size <= MAX_PICTURE_BYTES) return out;
    }
  } catch {
    return null;
  }
  return null;
}

// The bytes as base64, which is how JSON carries them.
function base64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).replace(/^data:[^,]*,/, ""));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

function previewURL(blob: Blob): string {
  return typeof URL.createObjectURL === "function" ? URL.createObjectURL(blob) : "";
}

function forget(url: string) {
  if (url && typeof URL.revokeObjectURL === "function") URL.revokeObjectURL(url);
}

interface ReportInfo {
  can_send: boolean;
  can_attach_log: boolean;
}

// Which client this is, as the report names it. The desktop window binds
// lancastClientNote; a browser has none.
function clientName(): string {
  if (typeof window.lancastClientNote === "function") return "desktop window";
  return window.matchMedia?.("(max-width: 600px)").matches ? "phone browser" : "browser";
}

export function ReportIssue({ version, onClose }: { version?: string; onClose: () => void }) {
  useBackHandler(onClose);
  const info = useQuery({
    queryKey: ["report-info"],
    queryFn: ({ signal }) => apiGet<ReportInfo>("/api/report", signal),
    staleTime: 60_000,
  });
  const [what, setWhat] = useState("");
  const [expected, setExpected] = useState("");
  const [steps, setSteps] = useState("");
  const [contact, setContact] = useState("");
  const [includeLog, setIncludeLog] = useState(false);
  const [showLog, setShowLog] = useState(false);
  const [copied, setCopied] = useState(false);
  const [pictures, setPictures] = useState<Picture[]>([]);
  const [pictureError, setPictureError] = useState<string | null>(null);
  const nextPicture = useRef(1);
  const fileInput = useRef<HTMLInputElement>(null);

  // A preview address holds its picture in memory until it is let go.
  const held = useRef<Picture[]>([]);
  held.current = pictures;
  useEffect(
    () => () => {
      for (const p of held.current) forget(p.url);
    },
    [],
  );

  const addPictures = async (files: Blob[]) => {
    setPictureError(null);
    const room = MAX_PICTURES - held.current.length;
    const images = files.filter((f) => f.type.startsWith("image/"));
    if (images.length < files.length) setPictureError("Only pictures can be attached.");
    if (images.length > room) setPictureError(`At most ${MAX_PICTURES} pictures.`);
    const added: Picture[] = [];
    for (const f of images.slice(0, Math.max(0, room))) {
      const blob = f.size > MAX_PICTURE_BYTES ? await shrink(f) : f;
      if (!blob) {
        setPictureError("A picture was too large to send and could not be made smaller.");
        continue;
      }
      added.push({ id: nextPicture.current++, blob, url: previewURL(blob) });
    }
    if (added.length > 0) setPictures((now) => [...now, ...added].slice(0, MAX_PICTURES));
  };

  const removePicture = (id: number) => {
    const gone = pictures.find((p) => p.id === id);
    if (gone) forget(gone.url);
    setPictures((now) => now.filter((p) => p.id !== id));
  };

  const log = useQuery({
    queryKey: ["report-log"],
    queryFn: ({ signal }) => apiGet<{ lines: string[] }>("/api/logs?lines=2000", signal),
    enabled: showLog && !!info.data?.can_attach_log,
  });

  const send = useMutation({
    mutationFn: async () =>
      apiPost<{ sent: boolean }>("/api/report", {
        what,
        expected,
        steps,
        contact,
        client: clientName(),
        include_log: includeLog && !!info.data?.can_attach_log,
        images: await Promise.all(pictures.map((p) => base64(p.blob))),
      }),
  });

  const canSend = !!info.data?.can_send;
  const ready = what.trim() !== "" && !send.isPending;

  const asText = () =>
    [
      "What happened:",
      what.trim(),
      "",
      "What you expected:",
      expected.trim(),
      "",
      "Steps to make it happen again:",
      steps.trim(),
      "",
      `LANcast ${version || "unknown"} · ${clientName()}`,
    ].join("\n");

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(asText());
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  if (send.isSuccess) {
    return (
      <div className="addpl__overlay" onClick={onClose}>
        <div className="addpl report" role="dialog" aria-label="Report sent" onClick={(e) => e.stopPropagation()}>
          <div className="addpl__head">
            <span className="section-label">Report an issue</span>
            <button className="addpl__x" onClick={onClose} aria-label="Close">
              ✕
            </button>
          </div>
          <p className="report__done">Sent. Thank you for taking the time.</p>
          <div className="report__actions">
            <button className="addpl__create" onClick={onClose} autoFocus>
              Close
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="addpl__overlay" onClick={onClose}>
      <div
        className="addpl report"
        role="dialog"
        aria-label="Report an issue"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
        }}
        // A screenshot pasted anywhere in the form is attached: the quickest
        // way here from Print Screen or Win+Shift+S.
        onPaste={(e) => {
          const files = [...(e.clipboardData?.items ?? [])]
            .filter((i) => i.kind === "file" && i.type.startsWith("image/"))
            .map((i) => i.getAsFile())
            .filter((f): f is File => f !== null);
          if (files.length === 0) return;
          e.preventDefault();
          void addPictures(files);
        }}
      >
        <div className="addpl__head">
          <span className="section-label">Report an issue</span>
          <button className="addpl__x" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>

        <label className="report__field">
          <span>What happened</span>
          <textarea
            className="addpl__input report__text"
            value={what}
            rows={4}
            autoFocus
            maxLength={4000}
            onChange={(e) => setWhat(e.target.value)}
          />
        </label>
        <label className="report__field">
          <span>What you expected</span>
          <textarea
            className="addpl__input report__text"
            value={expected}
            rows={2}
            maxLength={4000}
            onChange={(e) => setExpected(e.target.value)}
          />
        </label>
        <label className="report__field">
          <span>Steps to make it happen again</span>
          <textarea
            className="addpl__input report__text"
            value={steps}
            rows={3}
            maxLength={4000}
            onChange={(e) => setSteps(e.target.value)}
          />
        </label>
        <label className="report__field">
          <span>How to reach you, if you'd like a reply (optional)</span>
          <input
            className="addpl__input"
            value={contact}
            maxLength={200}
            onChange={(e) => setContact(e.target.value)}
          />
        </label>

        <div className="report__pictures">
          <div className="report__pictures-head">
            <span>Pictures (optional)</span>
            <button
              type="button"
              className="report__link"
              disabled={pictures.length >= MAX_PICTURES}
              onClick={() => fileInput.current?.click()}
            >
              Add pictures
            </button>
            <input
              ref={fileInput}
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              multiple
              hidden
              aria-label="Choose pictures to attach"
              onChange={(e) => {
                void addPictures([...(e.target.files ?? [])]);
                e.target.value = "";
              }}
            />
          </div>
          {pictures.length > 0 ? (
            <ul className="report__thumbs" aria-label="Attached pictures">
              {pictures.map((p, i) => (
                <li key={p.id} className="report__thumb">
                  {p.url && <img src={p.url} alt={`Picture ${i + 1}`} />}
                  <button
                    type="button"
                    className="report__thumb-x"
                    aria-label={`Remove picture ${i + 1}`}
                    onClick={() => removePicture(p.id)}
                  >
                    ✕
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="report__note">Up to four. You can also paste a screenshot here.</p>
          )}
          {pictureError && <p className="addpl__error">{pictureError}</p>}
        </div>

        {info.data?.can_attach_log && (
          <div className="report__log">
            <label className="report__check">
              <input type="checkbox" checked={includeLog} onChange={(e) => setIncludeLog(e.target.checked)} />
              Include the server log (the last 2,000 lines)
            </label>
            <p className="report__note">
              The log names this server's folders, addresses and accounts.{" "}
              <button type="button" className="report__link" onClick={() => setShowLog((v) => !v)}>
                {showLog ? "Hide it" : "Read it first"}
              </button>
            </p>
            {showLog && (
              <pre className="report__logview" aria-label="The log that would be sent">
                {log.isLoading ? "Loading…" : log.isError ? "The log could not be read." : (log.data?.lines ?? []).join("\n")}
              </pre>
            )}
          </div>
        )}

        {info.isError && <p className="addpl__error">{errorMessage(info.error)}</p>}
        {send.isError && <p className="addpl__error">{errorMessage(send.error)}</p>}

        {info.data && !canSend ? (
          <p className="report__note">
            This build of LANcast can't send reports. Copy the report and send it to the developer
            another way{pictures.length > 0 ? ", with the pictures attached yourself" : ""}.
          </p>
        ) : (
          <p className="report__note">
            Goes to the developer, with this LANcast's version, its operating system, which app you're
            using, and whether you're an administrator
            {pictures.length > 0 &&
              `, with the ${pictures.length === 1 ? "picture" : `${pictures.length} pictures`} above`}
            . Nothing is sent until you press Send.
          </p>
        )}

        <div className="report__actions">
          <button type="button" className="addpl__create report__copy" onClick={copy} disabled={what.trim() === ""}>
            {copied ? "Copied" : "Copy report"}
          </button>
          {canSend && (
            <button type="button" className="addpl__create" onClick={() => send.mutate()} disabled={!ready}>
              {send.isPending ? "Sending…" : "Send"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
