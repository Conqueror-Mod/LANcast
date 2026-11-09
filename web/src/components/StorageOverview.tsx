import { useMeasureStorage, useStorage } from "@/api/hooks";
import type { StoragePart } from "@/api/types";
import { formatBytes } from "@/lib/format";
import "./StorageOverview.css";

/*
 * What LANcast keeps on disk, part by part (settings-3-plan step 7).
 *
 * Read-only on purpose. Each part says what it is and whether it can be had
 * again — artwork and previews are rebuilt, game saves are not — because the
 * question a big number raises is "can I get rid of that?", and the honest
 * answer differs part to part. Where a setting governs a part, it is named.
 *
 * The bars are proportions of what LANcast holds, drawn in a neutral colour:
 * gold means where you are, and a bar is not a place.
 */

const PARTS: Record<string, { label: string; about: string }> = {
  database: {
    label: "Database",
    about: "Your libraries, accounts, watch history and every decision you made. Back it up; it cannot be rebuilt.",
  },
  artwork: {
    label: "Artwork",
    about: "Posters, backdrops and faces, with their resized copies. Fetched again if lost. The Artwork cache limit below governs it.",
  },
  previews: {
    label: "Scrub-bar previews",
    about: "The pictures shown while seeking. Made again from the films if lost.",
  },
  optimized: {
    label: "Optimized copies",
    about: "Films converted ahead of time to play anywhere. Made again if lost; their size limit is in Transcoding.",
  },
  backups: {
    label: "Backups",
    about: "Copies of the database, above. The schedule decides how many are kept.",
  },
  transcode: {
    label: "Conversion scratch",
    about: "Space conversions use while they run. Emptied as they finish.",
  },
  subtitles: {
    label: "Subtitles",
    about: "Subtitles taken out of films or downloaded.",
  },
  models: {
    label: "Face and picture-search models",
    about: "Downloaded when face recognition or picture search was set up.",
  },
  saves: {
    label: "Game saves",
    about: "Retro game saves. The one part here that cannot be fetched or made again.",
  },
  downloads: {
    label: "Tools and downloads",
    about: "Media tools, game cores and databases, plugins and visualizer presets. Fetched again if removed.",
  },
  logs: {
    label: "Logs",
    about: "The server's log and its rotations.",
  },
  other: {
    label: "Other",
    about: "Anything else in the data folder.",
  },
};

function ago(unix: number): string {
  const s = Math.max(0, Math.round(Date.now() / 1000 - unix));
  if (s < 60) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} minute${m === 1 ? "" : "s"} ago`;
  const h = Math.round(m / 60);
  return `${h} hour${h === 1 ? "" : "s"} ago`;
}

function PartRow({ part, total }: { part: StoragePart; total: number }) {
  const known = PARTS[part.id];
  const share = total > 0 ? part.bytes / total : 0;
  return (
    <li className={"storage__part" + (part.bytes === 0 ? " storage__part--empty" : "")}>
      <div className="storage__head">
        <span className="storage__label">{known?.label ?? part.id}</span>
        <span className="storage__size">{part.bytes === 0 ? "none" : formatBytes(part.bytes)}</span>
      </div>
      <div className="storage__bar" aria-hidden="true">
        <div className="storage__fill" style={{ width: `${(share * 100).toFixed(2)}%` }} />
      </div>
      <p className="set-row__sub">
        {known?.about}
        {part.outside && <> Kept outside the data folder, at {part.path}.</>}
      </p>
      {part.largest && part.largest.length > 0 && (
        <ul className="storage__largest">
          {part.largest.map((e) => (
            <li key={e.name}>
              <span className="storage__name">{e.name}</span>
              <span className="storage__size">{formatBytes(e.bytes)}</span>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

export function StorageOverview() {
  const { data, isError } = useStorage();
  const measure = useMeasureStorage();
  const report = data?.report ?? null;
  const measuring = data?.measuring || measure.isPending;

  return (
    <div className="storage" aria-busy={measuring}>
      {isError && <p className="set-row__note">The server would not say what it is storing.</p>}
      {!report && !isError && (
        <p className="set-row__sub" role="status">
          Measuring the data folder… this reads every file once and can take a minute.
        </p>
      )}
      {report && (
        <>
          <div className="set-row">
            <div className="set-row__main">
              <div className="set-row__title">
                {formatBytes(report.total_bytes)} in {report.data_dir}
              </div>
              <div className="set-row__sub" role="status">
                {report.disk_total_bytes > 0 &&
                  `${formatBytes(report.disk_free_bytes)} free of ${formatBytes(report.disk_total_bytes)} on that drive · `}
                {measuring ? "measuring again…" : `measured ${ago(report.measured_at)}`}
                {report.partial && " · stopped before the end, so these are at least the sizes shown"}
              </div>
            </div>
            <div className="set-row__actions">
              <button className="set-btn" disabled={measuring} onClick={() => measure.mutate()}>
                Measure again
              </button>
            </div>
          </div>
          <ul className="storage__parts">
            {report.parts.map((p) => (
              <PartRow key={p.id} part={p} total={report.total_bytes} />
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
