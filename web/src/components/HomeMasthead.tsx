import { Link } from "react-router";
import { useCurrentUser } from "@/api/hooks";
import { LibraryIcon } from "./LibraryIcon";
import { navCount } from "./AppShell";
import type { Library } from "@/api/types";
import "./HomeMasthead.css";

/*
 * The home page's own identity, rather than a stack of shelves that begins
 * mid-sentence.
 *
 * The brief was "more branded, thematic — beyond functional shelves", and the
 * temptation with that brief is decoration: a hero image, a colour wash, gold
 * everywhere. Gold is not available for this. It means *where you are* and
 * nothing else, and the moment it also means "this is the nice bit" the focus
 * ring stops being readable across the whole app. So the theme has to come from
 * the things this project already owns — the nebula field, the letterspaced
 * caps, the sense of a room with a floor and a horizon.
 *
 * What it actually does, beyond looking like something:
 *
 *   - Greets by name and by hour, which is the one line on the page that says
 *     the server knows who is asking.
 *   - Says how much is here, which is a true boast: it is the whole point of
 *     running your own server.
 *
 * The library buttons it used to carry moved to the resume console's empty
 * state (ADR 0118 §3, LibraryLinks below): with something to resume, the
 * first screen has a better answer than a list of destinations.
 *
 * It compresses when there is a hero below it, because two full-height
 * statements stacked is one too many and the hero is the better of the two.
 */
/*
 * The libraries as destinations, with their counts.
 *
 * This used to sit in the masthead on every visit. Since ADR 0118 the
 * libraries live in the navigation, and here only when there is nothing in
 * progress: a first screen with nothing to resume should answer "what is in
 * here", and one with something to resume already has.
 */
export function LibraryLinks({ libraries }: { libraries: Library[] }) {
  if (libraries.length === 0) return null;
  return (
    <nav className="masthead__libs" aria-label="Libraries">
      {libraries.map((lib) => (
        <Link key={lib.id} className="masthead__lib" to={`/library/${lib.id}`}>
          <span className="masthead__libicon" aria-hidden="true">
            <LibraryIcon kind={lib.kind} />
          </span>
          <span className="masthead__libname">{lib.name}</span>
          <span className="masthead__libcount">{navCount(lib).toLocaleString()}</span>
        </Link>
      ))}
    </nav>
  );
}

export function HomeMasthead({
  libraries,
  hasHero,
}: {
  libraries: Library[] | undefined;
  hasHero: boolean;
}) {
  const user = useCurrentUser();
  const libs = libraries ?? [];
  const total = libs.reduce((n, l) => n + navCount(l), 0);

  return (
    <header className={"masthead" + (hasHero ? " masthead--compact" : "")}>
      <div className="masthead__greeting">
        <span className="masthead__hello">
          {greeting()}
          {user?.name ? `, ${user.name}` : ""}
        </span>
        {/* The count is the boast, and it is a true one: it is the whole point
            of running your own server. Suppressed at zero, where it would read
            as an accusation. */}
        {total > 0 && (
          <span className="masthead__count">
            {total.toLocaleString()} things to play, all of them yours
          </span>
        )}
      </div>

    </header>
  );
}

/*
 * Time of day, from the *local* clock.
 *
 * Built from `getHours()` rather than from anything ISO — the recurring trap in
 * this project is a UTC date resolving to tomorrow in a US evening, and a
 * greeting that says "good morning" at nine at night is the same bug wearing a
 * friendlier face.
 */
function greeting(): string {
  const h = new Date().getHours();
  if (h < 5) return "Still up";
  if (h < 12) return "Good morning";
  if (h < 18) return "Good afternoon";
  return "Good evening";
}
