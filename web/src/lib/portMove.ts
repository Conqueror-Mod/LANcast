/*
 * Following the server to a new port (ADR 0089).
 *
 * The page cannot read a response from another port — that is another
 * origin — but it can tell whether anything answered: a no-cors request
 * resolves with an opaque response when a server replied, and rejects when
 * nothing did. That is all this needs, and it is why these are their own
 * module: tests replace them, since jsdom has no network and no navigation.
 */

/** The same page at another port. */
export function urlAtPort(port: string, loc: Location = window.location): string {
  const u = new URL(loc.href);
  u.port = port;
  return u.toString();
}

/** Whether a server answers at origin within ms. */
export async function answersWithin(origin: string, ms: number): Promise<boolean> {
  const deadline = Date.now() + ms;
  while (Date.now() < deadline) {
    try {
      await fetch(new URL("/api/health", origin).toString(), { mode: "no-cors", cache: "no-store" });
      return true;
    } catch {
      await new Promise((r) => setTimeout(r, 1000));
    }
  }
  return false;
}

export function goTo(url: string) {
  window.location.assign(url);
}
