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
 * server (no phone-home). It changes when the app does, which is what was
 * asked for, without fetching anything from GitHub.
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

  return (
    <>
      <section className="settings__section">
        <span className="section-label">About</span>
        <div className="about__head">
          <div className="about__name">LANcast</div>
          <div className="about__version">
            Server {about.version}
            {windowVersion && windowVersion !== about.version && (
              <> · this window {windowVersion}</>
            )}
          </div>
        </div>
        <p className="about__line">
          A self-hosted media server: your library, on your machines, asking nobody else's
          server for anything.
        </p>
        <p className="about__line">
          Free software under the GNU Affero General Public License v3.0, with a commercial
          licence available. The source is at{" "}
          <a href={about.repository} target="_blank" rel="noreferrer noopener">
            {about.repository.replace(/^https:\/\//, "")}
          </a>
          .
        </p>
      </section>

      <section className="settings__section">
        <span className="section-label">What's new in {about.version}</span>
        {about.notes ? (
          <div className="about__notes">{renderNotes(about.notes)}</div>
        ) : (
          <p className="about__muted">No release notes are recorded for this version.</p>
        )}
      </section>

      <section className="settings__section">
        <span className="section-label">Licence and third-party components</span>
        <details className="about__notice">
          <summary>Show the notice that ships with LANcast</summary>
          <pre>{about.notice}</pre>
        </details>
      </section>
    </>
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

/*
 * renderNotes draws release notes from the small part of Markdown they use:
 * headings, bullets (one level of nesting), paragraphs, **bold** and `code`.
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

// **bold** and `code`, as elements; everything else as text.
function inline(text: string): ReactNode[] {
  const parts: ReactNode[] = [];
  const re = /\*\*(.+?)\*\*|`([^`]+)`/g;
  let last = 0;
  let k = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) parts.push(text.slice(last, m.index));
    if (m[1] !== undefined) parts.push(<strong key={k++}>{m[1]}</strong>);
    else parts.push(<code key={k++}>{m[2]}</code>);
    last = m.index + m[0].length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return parts;
}
