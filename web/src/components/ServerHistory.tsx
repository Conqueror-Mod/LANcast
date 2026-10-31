import { useState } from "react";
import { useDashboardHistory } from "@/api/hooks";
import type { HistorySample } from "@/api/types";
import { hourly, nearest, niceMax, pathFor, segments, type Point } from "@/lib/historyChart";
import "./ServerHistory.css";

/*
 * What this server has been doing (ADR 0106): small multiples, one measure
 * each and one axis each, never two scales on one chart. A single series per
 * chart, so the title names it and there is no legend. The line is a blue
 * from the field, never gold: gold says where you are, and a graph is not.
 */

const RANGES = [6, 24, 48] as const;

interface Measure {
  key: string;
  title: string;
  value: (s: HistorySample) => number | null;
  format: (v: number) => string;
  fixedMax?: number;
  // What the tooltip adds under the value, from the same sample.
  detail?: (s: HistorySample) => string;
}

const whole = (v: number) => Math.round(v).toLocaleString();

const MEASURES: Measure[] = [
  { key: "watching", title: "People watching", value: (s) => s.watching, format: whole },
  {
    key: "conversions",
    title: "Conversions running",
    value: (s) => s.encoding + s.copying,
    format: whole,
    detail: (s) => `${s.encoding} re-encoding · ${s.copying} rewrapping`,
  },
  {
    key: "cpu",
    title: "CPU, whole machine",
    value: (s) => (s.cpu < 0 ? null : s.cpu),
    format: (v) => `${Math.round(v)}%`,
    fixedMax: 100,
  },
  { key: "memory", title: "Memory, this server", value: (s) => s.memory_mb, format: (v) => `${Math.round(v)} MB` },
  {
    key: "media",
    title: "Media sent",
    // A minute's bytes as an average rate, the unit a connection is sold in.
    value: (s) => ((s.media_bytes ?? 0) * 8) / 60 / 1e6,
    format: (v) => (v >= 10 ? `${Math.round(v)} Mbit/s` : `${v.toFixed(1)} Mbit/s`),
  },
  { key: "requests", title: "Requests a minute", value: (s) => s.requests, format: whole },
  { key: "errors", title: "Server errors a minute", value: (s) => s.errors, format: whole },
];

const clock = (t: number) =>
  new Date(t * 1000).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
const dayClock = (t: number) =>
  new Date(t * 1000).toLocaleString(undefined, { weekday: "short", hour: "numeric", minute: "2-digit" });

export function ServerHistory() {
  const [hours, setHours] = useState<(typeof RANGES)[number]>(6);
  const { data, isError } = useDashboardHistory(hours);
  const samples = data?.samples ?? [];
  // The minute in progress, read live, so each chart moves while it is open.
  const live = data?.now;
  const to = Math.floor(Date.now() / 1000);
  const from = to - hours * 3600;
  const label = hours > 24 ? dayClock : clock;

  return (
    <section className="settings__section">
      <div className="history__head">
        <span className="section-label">Server activity</span>
        <div className="history__range" role="group" aria-label="How far back">
          {RANGES.map((h) => (
            <button
              key={h}
              type="button"
              className={"history__range-btn" + (h === hours ? " is-on" : "")}
              aria-pressed={h === hours}
              onClick={() => setHours(h)}
            >
              {h} h
            </button>
          ))}
        </div>
      </div>
      {isError ? (
        <p className="set-row__sub">The server's history could not be read.</p>
      ) : samples.length === 0 && !live ? (
        <p className="set-row__sub">
          Nothing recorded yet. The server takes its first reading a minute after it starts, then one a
          minute, and keeps two days of them.
        </p>
      ) : (
        <>
          <div className="history__grid">
            {MEASURES.map((m) => (
              <HistoryChart key={m.key} measure={m} samples={samples} live={live} from={from} to={to} label={label} />
            ))}
          </div>
          <p className="set-row__sub">
            A reading a minute, kept for two days and through restarts. Gaps are when the server was not
            running. Media sent counts streams, conversions, downloads and the like, not pages or
            pictures.
          </p>
        </>
      )}
    </section>
  );
}

const W = 300;
const H = 90;

function HistoryChart({
  measure,
  samples,
  live,
  from,
  to,
  label,
}: {
  measure: Measure;
  samples: HistorySample[];
  live?: HistorySample;
  from: number;
  to: number;
  label: (t: number) => string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const points: Point[] = [];
  const byT = new Map<number, HistorySample>();
  for (const s of samples) {
    const v = measure.value(s);
    if (v !== null) {
      points.push({ t: s.at, v });
      byT.set(s.at, s);
    }
  }
  // The line ends at now, not at the last whole minute.
  const lastAt = samples.length ? samples[samples.length - 1].at : 0;
  if (live && live.at > lastAt) {
    const v = measure.value(live);
    if (v !== null) {
      points.push({ t: live.at, v });
      byT.set(live.at, live);
    }
  }
  const yMax = niceMax(points.map((p) => p.v), measure.fixedMax);
  const latest = points[points.length - 1];
  const peak = points.reduce((m, p) => Math.max(m, p.v), 0);
  const at = hover !== null ? nearest(points, hover) : undefined;
  const atX = at ? ((at.t - from) / Math.max(1, to - from)) * W : 0;

  return (
    <figure className="history__chart">
      <figcaption className="history__title">
        <span>{measure.title}</span>
        <span className="history__now">{latest ? measure.format(latest.v) : "—"}</span>
      </figcaption>
      {points.length === 0 ? (
        <p className="history__none">Not readable on this machine.</p>
      ) : (
        <div className="history__plot">
          <span className="history__ymax" aria-hidden="true">
            {measure.format(yMax)}
          </span>
          <svg
            viewBox={`0 0 ${W} ${H}`}
            preserveAspectRatio="none"
            role="img"
            aria-label={`${measure.title}: now ${latest ? measure.format(latest.v) : "unknown"}, peak ${measure.format(peak)}, from ${label(from)} to now.`}
            onPointerMove={(e) => {
              const r = e.currentTarget.getBoundingClientRect();
              if (r.width > 0) setHover(from + ((e.clientX - r.left) / r.width) * (to - from));
            }}
            onPointerLeave={() => setHover(null)}
          >
            {/* Recessive: a baseline and a faint top, nothing else. */}
            <line className="history__grid-line" x1={0} x2={W} y1={0.5} y2={0.5} />
            <line className="history__base" x1={0} x2={W} y1={H - 0.5} y2={H - 0.5} />
            {segments(points).map((seg, i) => (
              <path
                key={i}
                className="history__line"
                d={pathFor(seg, from, to, yMax, W, H)}
                vectorEffect="non-scaling-stroke"
              />
            ))}
            {at && (
              <line className="history__crosshair" x1={atX} x2={atX} y1={0} y2={H} vectorEffect="non-scaling-stroke" />
            )}
          </svg>
          {at && (
            <div
              className="history__tip"
              role="status"
              style={{ left: `clamp(60px, ${(atX / W) * 100}%, calc(100% - 60px))` }}
            >
              <strong>{measure.format(at.v)}</strong>
              <span>{label(at.t)}</span>
              {measure.detail && byT.get(at.t) && <span>{measure.detail(byT.get(at.t)!)}</span>}
            </div>
          )}
          <div className="history__axis" aria-hidden="true">
            <span>{label(from)}</span>
            <span>now</span>
          </div>
        </div>
      )}
      {points.length > 0 && (
        <details className="history__table">
          <summary>Show the numbers</summary>
          <table>
            <thead>
              <tr>
                <th scope="col">Hour</th>
                <th scope="col">Average</th>
                <th scope="col">Peak</th>
              </tr>
            </thead>
            <tbody>
              {hourly(points).map((row) => (
                <tr key={row.hour}>
                  <td>{label(row.hour)}</td>
                  <td>{measure.format(row.avg)}</td>
                  <td>{measure.format(row.max)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </details>
      )}
    </figure>
  );
}
