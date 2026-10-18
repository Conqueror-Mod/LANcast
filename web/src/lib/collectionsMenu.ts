import type { MenuAction } from "@/components/Menu";
import type { Item } from "@/api/types";

/*
 * "View collection", once per collection the title is in. One collection says
 * just that; several name themselves, since "View collection" twice would not
 * say which is which.
 */
export function collectionsMenu(
  item: Item,
  collections: Item[] | undefined,
  open: (id: number) => void,
): MenuAction[] {
  if (item.kind === "collection") return [];
  const list = Array.isArray(collections) ? collections.filter((c) => c.id !== item.id) : [];
  if (list.length === 1) return [{ label: "View collection", onSelect: () => open(list[0].id) }];
  return list.map((c) => ({ label: `View ${c.title}`, onSelect: () => open(c.id) }));
}
