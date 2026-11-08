import { useEditShelf, useHomeShelves, useItem, useLibraries, useUnpinShelf } from "@/api/hooks";
import { useHeroMode, usePinnedHero, type HeroMode } from "@/lib/heroMode";
import { useSpoilerMode, type SpoilerMode } from "@/lib/spoilers";
import { unwatchedShelfLibraries, useHomePrefs, useSetHomePrefs } from "@/lib/homePrefs";

/*
 * Your account › Home (settings-3-plan step 3, ADR 0120).
 *
 * Everything here is about what you want to see on your Home page, so it
 * follows you from screen to screen. It gathers what was scattered: the
 * spotlight and spoilers sat on the device's Display pane for want of a
 * per-account store, and pinned shelves could only be moved or unpinned from
 * a menu on Home itself, one shelf at a time.
 */

// The built-in shelves Home draws, in Home's order. Unwatched is one per film
// or show library, named after it, and added below.
const BUILTIN_SHELVES: Array<{ key: string; label: string; sub: string }> = [
  { key: "new", label: "New this week", sub: "What arrived in the last seven days." },
  {
    key: "rate",
    label: "Rate what you've watched",
    sub: "What you finished lately and have not rated yet.",
  },
  { key: "watchlist", label: "Watchlist", sub: "What you saved to watch later." },
];

function SpotlightSection() {
  const [mode, setMode] = useHeroMode();
  const [pinnedID, setPinned] = usePinnedHero();
  const { data: pinnedItem } = useItem(pinnedID);
  const [spoilers, setSpoilers] = useSpoilerMode();

  return (
    <section className="settings__section">
      <span className="section-label">What Home shows</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Spotlight</div>
          <div className="set-row__sub">
            What the big panel at the top of Home shows. Whatever it is set to,
            an item with no backdrop is never chosen, and a choice with nothing
            to show falls back to the next rather than leaving the panel empty.
          </div>
        </div>
        <div className="set-row__actions">
          <select
            className="set-input"
            aria-label="Homepage spotlight"
            value={mode}
            onChange={(e) => setMode(e.target.value as HeroMode)}
          >
            <option value="continue">Continue watching</option>
            <option value="recent">Recently added</option>
            <option value="recommended">Suggested</option>
            <option value="pinned">A film I pick</option>
          </select>
        </div>
      </div>

      {mode === "recommended" && (
        // Said plainly, because the alternative is letting somebody believe
        // there is a recommender here. There is not, and there is not going to
        // be one that phones home.
        <p className="set-row__sub">
          Suggestions come from what you are part-way through: something
          unwatched from the same library sharing a genre with it. Nothing is
          sent anywhere, and the spotlight always says which film a suggestion
          came from.
        </p>
      )}

      {mode === "pinned" && (
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">
              {pinnedItem ? pinnedItem.title : "Nothing pinned yet"}
            </div>
            <div className="set-row__sub">
              {pinnedItem
                ? "Pinned to your spotlight."
                : "Open a film or a show and choose Pin to homepage. Until then the spotlight carries on as Continue watching."}
            </div>
          </div>
          {pinnedID > 0 && (
            <div className="set-row__actions">
              <button className="set-btn" onClick={() => setPinned(0)}>
                Unpin
              </button>
            </div>
          )}
        </div>
      )}

      <label className="set-row set-row--stacked">
        <div className="set-row__main">
          <div className="set-row__title">Spoilers on a season page</div>
          <div className="set-row__sub">
            An episode's synopsis is written as a summary, not a tease, so the
            next one down the list can give away what you were about to watch.
            This applies only to episodes you have not started — two minutes in,
            you have already met whatever the first scene gives away.
          </div>
        </div>
        <div className="set-row__actions">
          <select
            className="set-input"
            value={spoilers}
            onChange={(e) => setSpoilers(e.target.value as SpoilerMode)}
            aria-label="Spoilers on a season page"
          >
            <option value="synopsis">Hide the synopsis</option>
            <option value="all">Hide the synopsis and the still</option>
            <option value="show">Show everything</option>
          </select>
        </div>
      </label>
    </section>
  );
}

function BuiltinShelvesSection() {
  const { data: prefs } = useHomePrefs();
  const set = useSetHomePrefs();
  const { data: libraries } = useLibraries();
  const hidden = prefs?.hidden_shelves ?? [];
  const account = prefs?.account === true;

  const shelves = [
    ...BUILTIN_SHELVES,
    ...unwatchedShelfLibraries(libraries).map((l) => ({
      key: `unwatched:${l.id}`,
      label: `Unwatched in ${l.name}`,
      sub: "A shuffled handful you have not seen yet.",
    })),
  ];

  function toggle(key: string, show: boolean) {
    const next = show ? hidden.filter((k) => k !== key) : [...hidden.filter((k) => k !== key), key];
    set.mutate({ hidden_shelves: next });
  }

  return (
    <section className="settings__section">
      <span className="section-label">Shelves on Home</span>
      <p className="set-row__sub">
        Continue watching and the panels beside the spotlight always show; an
        empty shelf is never drawn whatever it is set to here.
      </p>
      {shelves.map((s) => (
        <label className="set-toggle set-toggle--described" key={s.key}>
          <input
            type="checkbox"
            checked={!hidden.includes(s.key)}
            disabled={!account}
            onChange={(e) => toggle(s.key, e.target.checked)}
          />
          <span>
            <strong>{s.label}</strong>
            <span className="set-toggle__desc">{s.sub}</span>
          </span>
        </label>
      ))}
      {set.isError && (
        <p className="set-row__sub" role="alert">
          That did not save: {(set.error as Error).message}
        </p>
      )}
    </section>
  );
}

function PinnedShelvesSection() {
  const { data } = useHomeShelves();
  const { data: libraries } = useLibraries();
  const edit = useEditShelf();
  const unpin = useUnpinShelf();
  const shelves = data?.shelves ?? [];
  const busy = edit.isPending || unpin.isPending;

  return (
    <section className="settings__section">
      <span className="section-label">Pinned shelves</span>
      {shelves.length === 0 ? (
        <p className="set-row__sub">
          None yet. In a library, set the filters and sort you want and choose
          Pin to Home; the shelf appears here, under the built-in ones, in the
          order you set.
        </p>
      ) : (
        shelves.map((shelf, i) => {
          const lib = libraries?.find((l) => l.id === shelf.library_id);
          return (
            <div className="set-row" key={shelf.id}>
              <div className="set-row__main">
                <div className="set-row__title">{shelf.title}</div>
                <div className="set-row__sub">{lib ? lib.name : "A library"}</div>
              </div>
              <div className="set-row__actions">
                <button
                  className="set-btn"
                  aria-label={`Move ${shelf.title} up`}
                  disabled={busy || i === 0}
                  onClick={() => edit.mutate({ id: shelf.id, position: shelf.position - 1 })}
                >
                  Up
                </button>
                <button
                  className="set-btn"
                  aria-label={`Move ${shelf.title} down`}
                  disabled={busy || i === shelves.length - 1}
                  onClick={() => edit.mutate({ id: shelf.id, position: shelf.position + 1 })}
                >
                  Down
                </button>
                <button
                  className="set-btn set-btn--danger"
                  aria-label={`Unpin ${shelf.title}`}
                  disabled={busy}
                  onClick={() => unpin.mutate(shelf.id)}
                >
                  Unpin
                </button>
              </div>
            </div>
          );
        })
      )}
    </section>
  );
}

export function HomePane() {
  return (
    <>
      <SpotlightSection />
      <BuiltinShelvesSection />
      <PinnedShelvesSection />
    </>
  );
}
