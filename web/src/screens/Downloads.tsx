import { Link } from "react-router-dom";
import { useDownloads, downloadURL } from "@/lib/downloads";
import "./Downloads.css";

/*
 * The downloads page.
 *
 * What it is not, first, because that is the design: it is not a transfer
 * manager. Once a download starts the browser owns it — there is no progress to
 * read, nothing to cancel, and no way to know whether it finished. A page
 * drawing a progress bar over that would be drawing a guess, and a guess that
 * says 100% for a transfer that died at 40% is worse than no bar at all.
 *
 * So it is a receipt list: what this device asked for, when, and a link to ask
 * again. That answers the two questions the page actually gets — "did I already
 * pull that episode down" and "what was that film called" — and it answers them
 * honestly.
 *
 * Per device, not per account. The phone that downloaded something is the phone
 * that has the file; a server-side list would tell the desktop it had a copy it
 * has never seen.
 */
export function Downloads() {
  const [list, , clear] = useDownloads();

  return (
    <div className="browse downloads">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">Downloads</h1>
        <span className="browse__count">{list.length || ""}</span>
      </div>

      <p className="downloads__note">
        What this device has asked the server for. The files themselves are
        wherever your browser puts downloads — LANcast records the request, not
        the transfer.
      </p>

      {/*
        The empty state sits with the note above it rather than floating in the
        middle of the page. `browse__message` carries 40px of vertical padding,
        which is right in a grid where it stands alone on an otherwise empty
        screen and wrong here, directly beneath a paragraph that is already
        saying something — the two together read as one explanation with a hole
        punched through it.
      */}
      {list.length === 0 && (
        <p className="browse__message downloads__empty">
          Nothing yet. Any film, episode or track has a <strong>Download</strong>{" "}
          button on its page, which hands you the original file — never a
          re-encoded copy.
        </p>
      )}

      {/* Rendered only when it holds something: an empty list still carries its
          own padding, which is invisible dead space under the empty state. */}
      {list.length > 0 && (
      <div className="downloads__list">
        {list.map((r) => (
          <div className="downloads__row" key={r.itemId}>
            <div className="downloads__what">
              <Link className="downloads__title" to={`/item/${r.itemId}`}>
                {r.title}
              </Link>
              {r.detail && (
                <span className="downloads__detail">{r.detail}</span>
              )}
              {/* The filename the server proposed, so this list can be read
                  beside a Downloads folder and matched to it. */}
              <span className="downloads__file">{r.filename}</span>
            </div>

            <span className="downloads__when">
              {new Date(r.at).toLocaleDateString()}
            </span>

            <a
              className="downloads__again"
              href={downloadURL(r.itemId)}
              download={r.filename}
            >
              Download again
            </a>
          </div>
        ))}
      </div>
      )}

      {list.length > 0 && (
        <button className="downloads__clear" onClick={clear}>
          Clear this list
        </button>
      )}
      {list.length > 0 && (
        // Said plainly beside the button, because "clear" next to a list of
        // files reads as "delete the files" and this deletes nothing.
        <span className="downloads__clearnote">
          Removes the records only. Your downloaded files are untouched.
        </span>
      )}
    </div>
  );
}
