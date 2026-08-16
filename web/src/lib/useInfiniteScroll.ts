import { useEffect, type RefObject } from "react";

/*
 * Pull the next page in as a sentinel below the grid comes into view.
 *
 * Extracted from LibraryView, which had the only copy — and the Collections
 * page, built later against the same hook, simply never wired any of it. It
 * rendered page one, 120 items, and stopped: every collection after roughly "H"
 * was unreachable with nothing on screen to say so, which is precisely the
 * failure the original comment says this paging exists to remove. A behaviour
 * that has to be re-implemented at every call site eventually is not, so it
 * lives here now and both pages call it.
 *
 * The observer alone is not enough, and that is the subtle half worth keeping
 * together with the rest: observer callbacks are suppressed in a hidden or
 * throttled tab, and if they never arrive the grid stops silently. Scroll and
 * resize back it up, and the immediate call covers a first page too short to
 * fill the viewport — which is how a filtered listing can otherwise show a
 * handful of rows and never load the rest.
 */
export function useInfiniteScroll(
  sentinel: RefObject<HTMLElement | null>,
  opts: {
    hasNextPage: boolean;
    isFetchingNextPage: boolean;
    fetchNextPage: () => void;
  },
) {
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = opts;

  useEffect(() => {
    if (!hasNextPage || isFetchingNextPage) return;
    const el = sentinel.current;
    if (!el) return;

    // Near enough to the viewport that the next page should already be loading.
    const near = () =>
      el.getBoundingClientRect().top < window.innerHeight + 600;
    const maybeFetch = () => {
      if (near()) fetchNextPage();
    };

    const io = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) fetchNextPage();
      },
      { rootMargin: "600px" },
    );
    io.observe(el);

    window.addEventListener("scroll", maybeFetch, { passive: true });
    window.addEventListener("resize", maybeFetch);
    maybeFetch();

    return () => {
      io.disconnect();
      window.removeEventListener("scroll", maybeFetch);
      window.removeEventListener("resize", maybeFetch);
    };
  }, [sentinel, hasNextPage, isFetchingNextPage, fetchNextPage]);
}
