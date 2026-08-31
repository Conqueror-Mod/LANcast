import type { Item } from "@/api/types";
import "./DesignBench.css";

/*
 * The design bench — a dev-only page for judging the look.
 *
 * It exists because the look cannot be reviewed the way everything else is.
 * The client suite runs in jsdom, which performs no layout and paints no
 * colour, so it can prove a heading exists and never that it is legible on the
 * field behind it. And the real screens all sit behind the auth gate, which
 * means the only way to see a colour change was to sign in to a live server and
 * find a film that happened to exercise it.
 *
 * So: the real components and the real stylesheets, against fixtures, in front
 * of the gate. Every surface that carries the identity on one screen, which is
 * what makes a palette change reviewable in one screenshot instead of eleven.
 *
 * `import.meta.env.DEV` keeps it out of the shipped bundle — Vite eliminates
 * the branch at build time, so this file is not in the production client at
 * all rather than merely unreachable within it.
 */

const fixture: Item = {
  id: 1,
  library_id: 1,
  kind: "movie",
  title: "The Long Way Down",
  year: 1998,
  parent_id: null,
  overview:
    "A returning astronaut discovers that the six months she spent alone in orbit have been edited out of every record on Earth, and that the only person who remembers her leaving is the one who signed the order.",
  content_rating: "PG-13",
  rating: 7.8,
  duration_ms: 7_020_000,
  artwork: {},
} as Item;

function Swatch({ name }: { name: string }) {
  return (
    <div className="bench__swatch">
      <div
        className="bench__chip"
        style={{ background: `var(--${name})` }}
        title={name}
      />
      <code>{name}</code>
    </div>
  );
}

/*
 * The type ramp on the field it is actually read on.
 *
 * This is the panel the whole bench was built for. The complaint that started
 * this work was blue-grey text on a blue field, and that is invisible in every
 * other kind of review: it passes a contrast check against black, it passes
 * every test in the suite, and it only fails when somebody looks at it.
 */
function TypeRamp() {
  return (
    <div className="bench__panel">
      <h2 className="bench__h">Type, on the field</h2>
      <p className="bench__t bench__t--primary">
        Primary — a title, a heading, the thing you are reading
      </p>
      <p className="bench__t bench__t--secondary">
        Secondary — a synopsis, the body of a description, a paragraph
      </p>
      <p className="bench__t bench__t--muted">
        Muted — a runtime, a year, a codec, the metadata line under a title
      </p>
      <p className="bench__t bench__t--faint">
        Faint — a caption, a hint, the least important thing on the screen
      </p>
    </div>
  );
}

export function DesignBench() {
  return (
    <div className="bench">
      {/*
        No field of its own: main.tsx renders the nebula and the starfield for
        every route, this one included. The bench used to draw a second copy,
        which doubled every alpha in it — so the field looked richer on this
        page than it does in the app, which is the one thing a design bench
        must never do.
      */}
      <header className="bench__head">
        <h1>Design bench</h1>
        <p>
          The real components and stylesheets against fixtures. Not shipped —
          dev builds only.
        </p>
      </header>

      <section className="bench__section">
        <h2 className="bench__h">Field &amp; elevation</h2>
        <div className="bench__row">
          <div className="bench__card bench__card--1">elev-1</div>
          <div className="bench__card bench__card--2">elev-2</div>
          <div className="bench__card bench__card--3">elev-3</div>
        </div>
      </section>

      <section className="bench__section">
        <h2 className="bench__h">Palette</h2>
        <div className="bench__row bench__row--wrap">
          {[
            "space-void",
            "space-deep",
            "space-raised",
            "nebula-blue",
            "nebula-violet",
            "nebula-indigo",
            "gold",
            "gold-bright",
            "text-primary",
            "text-secondary",
            "text-muted",
            "text-faint",
          ].map((n) => (
            <Swatch key={n} name={n} />
          ))}
        </div>
      </section>

      <section className="bench__section">
        <TypeRamp />
      </section>

      {/*
        A detail header in miniature: the exact arrangement the report was
        about — a title, a metadata line, a synopsis, and buttons, all on the
        field rather than on a card.
      */}
      <section className="bench__section">
        <h2 className="bench__h">Detail header</h2>
        <div className="bench__detail">
          <div className="bench__poster" />
          <div className="bench__meta">
            <h3 className="bench__title">{fixture.title}</h3>
            <div className="bench__facts">
              <span>1998</span>
              <span>·</span>
              <span>1h 57m</span>
              <span>·</span>
              <span>PG-13</span>
              <span>·</span>
              <span>★ 7.8</span>
            </div>
            <p className="bench__overview">{fixture.overview}</p>
            <div className="bench__actions">
              <button className="bench__btn bench__btn--primary">Play</button>
              <button className="bench__btn">Trailer</button>
              <button className="bench__btn">Mark watched</button>
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
