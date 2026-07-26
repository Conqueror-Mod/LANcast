import { useBackHandler } from "@/focus/FocusController";
import type { Trailer } from "@/api/types";
import "./TrailerModal.css";

// Plays a provider trailer in a lightbox over the detail screen. The server
// returns only the video's identity (never a proxied stream), so the client
// embeds it directly — playing it is the client's choice, and LANcast does not
// sit between the viewer and YouTube.
function embedURL(t: Trailer): string | null {
  if (t.site === "YouTube") {
    return `https://www.youtube-nocookie.com/embed/${encodeURIComponent(t.key)}?autoplay=1&rel=0`;
  }
  if (t.site === "Vimeo") {
    return `https://player.vimeo.com/video/${encodeURIComponent(t.key)}?autoplay=1`;
  }
  return null;
}

export function TrailerModal({
  trailer,
  title,
  onClose,
}: {
  trailer: Trailer;
  title: string;
  onClose: () => void;
}) {
  useBackHandler(onClose);
  const url = embedURL(trailer);

  return (
    <div className="trailer__overlay" onClick={onClose}>
      <div className="trailer__box" onClick={(e) => e.stopPropagation()}>
        <button className="trailer__x" onClick={onClose} aria-label="Close trailer">
          ×
        </button>
        <div className="trailer__frame">
          {url ? (
            <iframe
              src={url}
              title={`${title} — trailer`}
              allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
              allowFullScreen
            />
          ) : (
            <p className="trailer__unsupported">
              This trailer is hosted on {trailer.site}, which cannot be embedded.
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
