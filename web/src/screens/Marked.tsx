import { useRef } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMarkedItems, useTags, type ItemMark } from "@/api/hooks";
import { useInfiniteScroll } from "@/lib/useInfiniteScroll";
import { useBackHandler } from "@/focus/FocusController";
import { PosterTile } from "@/components/PosterTile";
import { useItemActions } from "@/components/itemActions";
import "./Browse.css";

/*
 * Everything you favourited, or everything carrying one of your tags, from
 * every library at once.
 *
 * Reached from the profile. Inside a library a tag is a filter on that
 * library's grid; this is the other question, "what have I marked", which does
 * not stop at a library's edge. It lists any level, so a favourited song sits
 * beside a favourited film.
 *
 * Private to the signed-in account like the marks themselves (ADR 0062): the
 * server reads the caller's own and nothing else, so there is no account here
 * to get wrong.
 */
export function Favourites() {
  return <Marked mark={{ favourite: true }} title="Favourites" />;
}

export function TagItems() {
  const { id } = useParams();
  const tagID = Number(id) || 0;
  const { data } = useTags();
  const tag = data?.tags.find((t) => t.id === tagID);
  return (
    <Marked
      mark={{ tag: tagID }}
      title={tag?.name ?? "Tag"}
      // A tag with nothing on it is removed from the list, so an unknown id is
      // usually a tag whose last item was untagged in another tab.
      gone={!!data && !tag}
    />
  );
}

function Marked({
  mark,
  title,
  gone = false,
}: {
  mark: ItemMark;
  title: string;
  gone?: boolean;
}) {
  const { actions, dialogs } = useItemActions();
  const navigate = useNavigate();
  const { data, isLoading, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useMarkedItems(mark);
  const items = (data?.pages ?? []).flatMap((p) => p.items);
  const total = data?.pages[0]?.total ?? 0;

  const sentinel = useRef<HTMLDivElement | null>(null);
  useInfiniteScroll(sentinel, { hasNextPage, isFetchingNextPage, fetchNextPage });

  const back = () => navigate("/profile");
  useBackHandler(back);

  return (
    <div className="browse">
      <div className="browse__head browse__head--sticky">
        <button className="pl-back" onClick={back}>
          ← Profile
        </button>
        <h1 className="browse__title">{title}</h1>
        <span className="browse__count">{total ? total.toLocaleString() : ""}</span>
      </div>

      {isError && <p className="browse__message">This list could not be loaded.</p>}

      {!isLoading && !isError && items.length === 0 && (
        <p className="browse__message">
          {"tag" in mark
            ? gone
              ? "Nothing carries this tag any more."
              : "Nothing carries this tag yet."
            : "Nothing favourited yet. Press the heart on any title's page to add it here."}
        </p>
      )}

      <div className="browse__grid">
        {items.map((item) => (
          <PosterTile key={item.id} item={item} actions={actions} />
        ))}
      </div>
      <div ref={sentinel} aria-hidden="true" />
      {dialogs}
    </div>
  );
}
