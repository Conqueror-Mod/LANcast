import { useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiGet } from "@/api/client";
import type { components } from "@/api/schema";
import type { DesktopVersion } from "@/lib/clientVersion";
import "./AboutSettings.css";

type About = components["schemas"]["About"];

/*
 * Settings › About: what this is, under what licence, and what changed in it.
 *
 * Everything comes from the server's GET /api/about, which was embedded when
 * the binary was built — the notes are the ones committed for this release —
 * so the pane describes the version that is running and asks nobody else's
 * server (no phone-home).
 *
 * Laid out as panels and tables rather than running text. The first version
 * was one column of prose and read as a wall: reported as "rough and flat".
 * The facts are a table because they are facts, each release section is a
 * card because it is a unit, and the third-party list is parsed out of NOTICE
 * into tables because it already is one, just drawn in spaces.
 */
export function AboutSettings() {
  const { data: about, isError } = useQuery({
    queryKey: ["about"],
    queryFn: ({ signal }) => apiGet<About>("/api/about", signal),
    staleTime: Infinity,
  });
  const windowVersion = useWindowVersion();

  if (isError) {
    return (
      <section className="settings__section">
        <span className="section-label">About</span>
        <p className="about__muted">The server did not say what it is. Try again in a moment.</p>
      </section>
    );
  }
  if (!about) return null;

  const sections = splitSections(about.notes ?? "");
  const groups = parseNotice(about.notice);

  return (
    <div className="about">
      <section className="about__panel about__hero">
        <div className="about__mark" aria-hidden="true">
          L
        </div>
        <div>
          <h2 className="about__name">LANcast</h2>
          <p className="about__tagline">
            Your library, on your machines, asking nobody else's server for anything.
          </p>
        </div>
      </section>

      <section className="about__panel">
        <span className="section-label">This installation</span>
        <table className="about__facts">
          <tbody>
            <tr>
              <th scope="row">Server</th>
              <td>{about.version}</td>
            </tr>
            {windowVersion && (
              <tr>
                <th scope="row">This window</th>
                <td>
                  {windowVersion}
                  {windowVersion !== about.version && (
                    <span className="about__chip">differs from the server</span>
                  )}
                </td>
              </tr>
            )}
            <tr>
              <th scope="row">Licence</th>
              <td>GNU Affero General Public License v3.0 · commercial licence available</td>
            </tr>
            <tr>
              <th scope="row">Source</th>
              <td>
                <a href={about.repository} target="_blank" rel="noreferrer noopener">
                  {about.repository.replace(/^https:\/\//, "")}
                </a>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section className="about__block">
        <span className="section-label">What's new in {about.version}</span>
        {sections.length > 0 ? (
          <div className="about__cards about__cards--flow">
            {sections.map((sec, i) => (
              <article className="about__panel about__card" key={i}>
                {sec.title && <h3 className="about__card-title">{inline(sec.title)}</h3>}
                <div className="about__notes">{renderNotes(sec.body)}</div>
              </article>
            ))}
          </div>
        ) : (
          <p className="about__muted">No release notes are recorded for this version.</p>
        )}
      </section>

      <section className="about__block">
        <span className="section-label">Built with</span>
        <div className="about__cards about__cards--wide">
          {groups.map((g) => (
            <article className="about__panel" key={g.title}>
              <h3 className="about__card-title">{g.title}</h3>
              <table className="about__components">
                <thead>
                  <tr>
                    <th scope="col">Component</th>
                    <th scope="col">Licence</th>
                  </tr>
                </thead>
                <tbody>
                  {g.rows.map((r) => (
                    <tr key={r.name}>
                      <td>{r.name}</td>
                      <td>
                        {r.licence}
                        {r.note && <div className="about__note">{r.note}</div>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </article>
          ))}
        </div>
        <details className="about__notice">
          <summary>The full notice, as it ships</summary>
          <pre>{about.notice}</pre>
        </details>
      </section>
    </div>
  );
}

// The desktop window's own version, or null in a browser tab.
function useWindowVersion(): string | null {
  const [v, setV] = useState<string | null>(null);
  useEffect(() => {
    const state = (window as { lancastDesktopState?: () => Promise<DesktopVersion> })
      .lancastDesktopState;
    if (!state) return;
    let live = true;
    void state()
      .then((s) => {
        if (live) setV(s?.client_version ?? null);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);
  return v;
}

/** The notes cut at their headings: one card per section. */
export function splitSections(md: string): { title: string; body: string }[] {
  const out: { title: string; body: string }[] = [];
  let cur: { title: string; lines: string[] } | null = null;
  for (const line of md.split(/\r?\n/)) {
    const h = /^#{1,6}\s+(.*)$/.exec(line);
    if (h) {
      if (cur) out.push({ title: cur.title, body: cur.lines.join("\n") });
      cur = { title: h[1].trim(), lines: [] };
    } else {
      if (!cur) cur = { title: "", lines: [] };
      cur.lines.push(line);
    }
  }
  if (cur) out.push({ title: cur.title, body: cur.lines.join("\n") });
  return out.filter((s) => s.title || s.body.trim());
}

type Component = { name: string; licence: string; note: string };

/*
 * parseNotice reads NOTICE's third-party tables back out of their spacing.
 *
 * NOTICE is a plain-text file meant to be read as one, so it lays out two
 * columns with spaces: a group heading ends with a colon at the margin, a row
 * starts two spaces in with the licence after a run of spaces, a line indented
 * to the licence column continues the licence, and a line indented four is a
 * paragraph about the group, which the full notice below still shows. A name
 * that opens a bracket it has not closed continues on the next row.
 */
export function parseNotice(notice: string): { title: string; rows: Component[] }[] {
  const lines = notice.split(/\r?\n/);
  const start = lines.findIndex((l) => l.trim() === "Third-party components");
  const groups: { title: string; rows: Component[] }[] = [];
  let group: { title: string; rows: Component[] } | null = null;
  let last: Component | null = null;
  // Where this group's licence column starts, learned from its rows. A row
  // whose name runs right up to it has a single space before the licence, so
  // only the column can split it ("Emulator cores (mGBA, Mesen, bsnes, GPL-2.0").
  let col = 0;
  const open = (s: string) => (s.match(/\(/g)?.length ?? 0) > (s.match(/\)/g)?.length ?? 0);

  for (const line of lines.slice(start + 1)) {
    if (/^\S.*:\s*$/.test(line)) {
      group = { title: line.trim().replace(/:$/, "").replace(/\s*\(.*\)$/, ""), rows: [] };
      groups.push(group);
      last = null;
      col = 0;
      continue;
    }
    if (!group || line.trim() === "") continue;
    const indent = line.length - line.trimStart().length;
    if (indent >= 20 && last) {
      // The licence column, continued.
      if (last.licence) last.note = (last.note ? last.note + " " : "") + line.trim();
      else last.licence = line.trim();
      continue;
    }
    let row = /^ {2}(\S.*?)\s{2,}(\S.*)$/.exec(line);
    if (row) {
      col = line.length - row[2].length;
    } else if (indent === 2 && col > 2 && line.length > col) {
      row = ["", line.slice(2, col).trim(), line.slice(col).trim()] as unknown as RegExpExecArray;
    }
    if (row) {
      if (last && open(last.name)) {
        last.name += " " + row[1].trim();
        last.note = (last.note ? last.note + " " : "") + row[2].trim();
      } else {
        last = { name: row[1].trim(), licence: row[2].trim(), note: "" };
        group.rows.push(last);
      }
      continue;
    }
    if (indent === 2 && last && open(last.name)) last.name += " " + line.trim();
    // Anything else is a paragraph about the group.
  }
  return groups.filter((g) => g.rows.length > 0);
}

/*
 * renderNotes draws release notes from the small part of Markdown they use:
 * headings, bullets (one level of nesting), paragraphs, and inline **bold**,
 * *italic* and `code`, which nest.
 *
 * As React elements and text, never as HTML. The notes are this project's own
 * words, but a renderer that injects markup is one string away from being a
 * way in, and nothing here needs more than this.
 */
export function renderNotes(md: string): ReactNode[] {
  const out: ReactNode[] = [];
  let list: { text: string; nested: string[] }[] = [];
  let para: string[] = [];
  let key = 0;

  const flushPara = () => {
    if (para.length > 0) {
      out.push(<p key={key++}>{inline(para.join(" "))}</p>);
      para = [];
    }
  };
  const flushList = () => {
    if (list.length > 0) {
      out.push(
        <ul key={key++}>
          {list.map((li, i) => (
            <li key={i}>
              {inline(li.text)}
              {li.nested.length > 0 && (
                <ul>
                  {li.nested.map((n, j) => (
                    <li key={j}>{inline(n)}</li>
                  ))}
                </ul>
              )}
            </li>
          ))}
        </ul>,
      );
      list = [];
    }
  };

  for (const raw of md.split(/\r?\n/)) {
    const line = raw.replace(/\s+$/, "");
    const heading = /^#{1,6}\s+(.*)$/.exec(line);
    const nested = /^\s{2,}[-*]\s+(.*)$/.exec(line);
    const bullet = /^[-*]\s+(.*)$/.exec(line);
    if (heading) {
      flushPara();
      flushList();
      out.push(<h4 key={key++}>{inline(heading[1])}</h4>);
    } else if (nested && list.length > 0) {
      list[list.length - 1].nested.push(nested[1]);
    } else if (bullet) {
      flushPara();
      list.push({ text: bullet[1], nested: [] });
    } else if (line.trim() === "") {
      flushPara();
      flushList();
    } else if (list.length > 0 && /^\s+/.test(raw)) {
      // A wrapped bullet continues the bullet.
      list[list.length - 1].text += " " + line.trim();
    } else {
      flushList();
      para.push(line.trim());
    }
  }
  flushPara();
  flushList();
  return out;
}

// **bold**, *italic* and `code`, as elements and nesting; everything else as
// text. Code is literal: nothing inside backticks is read as markup.
export function inline(text: string): ReactNode[] {
  const parts: ReactNode[] = [];
  const re = /`([^`]+)`|\*\*(.+?)\*\*|\*([^*\s](?:[^*]*[^*\s])?)\*/g;
  let last = 0;
  let k = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) parts.push(text.slice(last, m.index));
    if (m[1] !== undefined) parts.push(<code key={k++}>{m[1]}</code>);
    else if (m[2] !== undefined) parts.push(<strong key={k++}>{inline(m[2])}</strong>);
    else parts.push(<em key={k++}>{inline(m[3])}</em>);
    last = m.index + m[0].length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return parts;
}
