import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useClearPicture, useCurrentUser, useSetAvatar, useUploadPicture } from "@/api/hooks";
import { AVATARS, AvatarGlyph } from "./Avatar";
import { useBackHandler, useFocusScope, useFocusZone } from "@/focus/FocusController";
import { initialCrop, MAX_ZOOM, pan, sourceRect, zoomTo, type Crop } from "@/lib/pictureCrop";
import "./RemoveDialog.css";
import "./PictureEditor.css";

/*
 * Choose a picture of yourself, and the square of it to keep (ADR 0121).
 *
 * The crop happens here and only the square is sent: a 512 px JPEG drawn on a
 * canvas, whatever the phone took. The server decodes and re-draws it anyway,
 * because a client is not a boundary; this just means a 12 MB photo becomes a
 * small upload, and anything the browser can open will do, HEIC included
 * where the browser reads it.
 *
 * Three ways to drive it, as the plan asks (profile-2-plan §3c): drag and the
 * wheel with a mouse, drag and the slider by touch, and with a remote or
 * keyboard the frame takes arrows (move) and + / − (zoom). The frame stops
 * those keys reaching the focus controller, so moving the picture does not
 * also move focus.
 *
 * In the page rather than a native dialog, like every question this app asks:
 * a native dialog in the frameless desktop window strands keyboard focus.
 */

const FRAME = 260;
const OUT = 512;
const STEP = 12;

export function PictureEditor({ onClose }: { onClose: () => void }) {
  const upload = useUploadPicture();
  const user = useCurrentUser();
  const setAvatar = useSetAvatar();
  const clearPicture = useClearPicture();
  const avatar = setAvatar.isPending ? setAvatar.variables : (user?.avatar ?? "");
  const fileInput = useRef<HTMLInputElement>(null);
  const chooseBtn = useRef<HTMLButtonElement>(null);
  const frame = useRef<HTMLDivElement>(null);
  const [img, setImg] = useState<HTMLImageElement | null>(null);
  const [url, setUrl] = useState<string | null>(null);
  const [crop, setCrop] = useState<Crop | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const drag = useRef<{ x: number; y: number } | null>(null);

  const close = useCallback(() => {
    if (!upload.isPending) onClose();
  }, [onClose, upload.isPending]);
  useBackHandler(close);
  /*
   * A remote reaches every control here and nothing behind the dialog. The
   * frame keeps the arrows for moving the picture, so on it OK saves and Back
   * cancels (profile-2-plan §3c).
   */
  const zone = useFocusZone();
  const scope = useFocusScope();
  const dialogRef = useCallback(
    (el: HTMLDivElement | null) => {
      zone(el);
      scope(el);
    },
    [zone, scope],
  );

  useEffect(() => {
    chooseBtn.current?.focus();
  }, []);
  // The object URL lives as long as the picture it points at.
  useEffect(() => () => void (url && URL.revokeObjectURL(url)), [url]);

  const choose = (file: File | undefined) => {
    if (!file) return;
    setProblem(null);
    const next = URL.createObjectURL(file);
    const el = new Image();
    el.onload = () => {
      if (el.naturalWidth < 16 || el.naturalHeight < 16) {
        setProblem("That picture is too small. Choose one at least 16 pixels across.");
        URL.revokeObjectURL(next);
        return;
      }
      setUrl(next);
      setImg(el);
      setCrop(initialCrop(el.naturalWidth, el.naturalHeight, FRAME));
      requestAnimationFrame(() => frame.current?.focus());
    };
    el.onerror = () => {
      URL.revokeObjectURL(next);
      setProblem("This browser can't open that file. Try a JPEG or PNG.");
    };
    el.src = next;
  };

  const save = () => {
    if (!img || !crop) return;
    const { sx, sy, side } = sourceRect(crop);
    const canvas = document.createElement("canvas");
    canvas.width = OUT;
    canvas.height = OUT;
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      setProblem("The picture could not be prepared.");
      return;
    }
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(img, sx, sy, side, side, 0, 0, OUT, OUT);
    canvas.toBlob(
      (blob) => {
        if (!blob) {
          setProblem("The picture could not be prepared.");
          return;
        }
        upload.mutate(blob, { onSuccess: onClose });
      },
      "image/jpeg",
      0.92,
    );
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (!crop) return;
    const step = e.shiftKey ? STEP * 4 : STEP;
    let next: Crop | null = null;
    switch (e.key) {
      case "ArrowLeft":
        next = pan(crop, step, 0);
        break;
      case "ArrowRight":
        next = pan(crop, -step, 0);
        break;
      case "ArrowUp":
        next = pan(crop, 0, step);
        break;
      case "ArrowDown":
        next = pan(crop, 0, -step);
        break;
      case "+":
      case "=":
      case "PageUp":
        next = zoomTo(crop, crop.zoom + 0.1);
        break;
      case "-":
      case "_":
      case "PageDown":
        next = zoomTo(crop, crop.zoom - 0.1);
        break;
      case "Enter":
        e.preventDefault();
        e.stopPropagation();
        if (!upload.isPending) save();
        return;
      default:
        return;
    }
    // Ours, not the focus controller's: moving the picture is not moving focus.
    e.preventDefault();
    e.stopPropagation();
    setCrop(next);
  };

  const scale = crop ? (FRAME / Math.min(crop.w, crop.h)) * crop.zoom : 1;
  const error =
    problem ??
    (upload.isError ? (upload.error as Error).message || "The picture could not be saved." : null) ??
    (setAvatar.isError || clearPicture.isError ? "That could not be saved." : null);

  return createPortal(
    <div className="removedlg__overlay" onClick={close}>
      <div
        ref={dialogRef}
        className="removedlg picdlg"
        role="dialog"
        aria-modal="true"
        aria-label="Your picture"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="removedlg__head">
          <span className="section-label">Your picture</span>
          <button type="button" className="removedlg__x" onClick={close} aria-label="Close">
            ✕
          </button>
        </div>

        <input
          ref={fileInput}
          type="file"
          accept="image/*"
          hidden
          data-testid="picture-file"
          onChange={(e) => {
            choose(e.target.files?.[0]);
            e.target.value = "";
          }}
        />

        {crop && url ? (
          <>
            <div
              ref={frame}
              className="picdlg__frame"
              style={{ width: FRAME, height: FRAME }}
              tabIndex={0}
              role="group"
              aria-label="Picture position. Arrow keys move it, plus and minus zoom, Enter saves."
              onKeyDown={onKey}
              onPointerDown={(e) => {
                drag.current = { x: e.clientX, y: e.clientY };
                e.currentTarget.setPointerCapture(e.pointerId);
              }}
              onPointerMove={(e) => {
                const d = drag.current;
                if (!d) return;
                setCrop((c) => (c ? pan(c, e.clientX - d.x, e.clientY - d.y) : c));
                drag.current = { x: e.clientX, y: e.clientY };
              }}
              onPointerUp={() => (drag.current = null)}
              onPointerCancel={() => (drag.current = null)}
              onWheel={(e) => setCrop((c) => (c ? zoomTo(c, c.zoom - e.deltaY / 500) : c))}
            >
              <img
                className="picdlg__img"
                src={url}
                alt=""
                draggable={false}
                style={{
                  left: crop.x,
                  top: crop.y,
                  width: crop.w * scale,
                  height: crop.h * scale,
                }}
              />
              <span className="picdlg__ring" aria-hidden="true" />
            </div>
            <label className="picdlg__zoom">
              <span>Zoom</span>
              <input
                type="range"
                min={1}
                max={MAX_ZOOM}
                step={0.01}
                value={crop.zoom}
                onChange={(e) => setCrop(zoomTo(crop, Number(e.target.value)))}
              />
            </label>
            <p className="removedlg__note picdlg__hint">
              Drag to move. Only the circle shows next to your name. Location and other
              details in the photo are not kept.
            </p>
          </>
        ) : (
          <>
            <p className="removedlg__note">
              A photo of you, or anything you like. Everyone signed in to this server can see it.
            </p>
            {user?.picture && (
              <button
                type="button"
                className="picdlg__remove"
                disabled={clearPicture.isPending}
                onClick={() => clearPicture.mutate()}
              >
                Remove your picture
              </button>
            )}
            {/*
              The drawn animals, here since the profile got its header
              (profile-2-plan §4a). Shown under a picture when there is one,
              because removing it brings the animal back.

              Gold marks the chosen one: it is *where you are* in this set,
              the same thing a selected rail row says. Pressing it again
              clears it, so there is no separate "none".
            */}
            <span className="picdlg__orlabel">
              {user?.picture ? "Shown when you have no picture" : "Or choose one of these"}
            </span>
            <div className="picdlg__animals" role="group" aria-label="Choose a drawn picture">
              {AVATARS.map((a) => {
                const on = avatar === a.key;
                return (
                  <button
                    key={a.key}
                    type="button"
                    className={"picdlg__animal" + (on ? " is-on" : "")}
                    aria-pressed={on}
                    aria-label={a.label}
                    title={on ? `${a.label} — press again to remove` : a.label}
                    disabled={setAvatar.isPending}
                    onClick={() => setAvatar.mutate(on ? "" : a.key)}
                  >
                    <AvatarGlyph avatar={a.key} size={26} />
                  </button>
                );
              })}
            </div>
          </>
        )}

        {error && <p className="removedlg__error">{error}</p>}

        <div className="picdlg__actions">
          <button
            ref={chooseBtn}
            type="button"
            className="picdlg__choose"
            onClick={() => fileInput.current?.click()}
            disabled={upload.isPending}
          >
            {crop ? "Choose another" : "Choose a picture…"}
          </button>
          {crop && (
            <button
              type="button"
              className="picdlg__save"
              onClick={save}
              disabled={upload.isPending}
            >
              {upload.isPending ? "Saving…" : "Save picture"}
            </button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
