/*
 * Retro consoles (ADR 0073): the values `Item.platform` carries, in the order
 * a Console filter shows them, with the names a person reads.
 *
 * The server's set is open, so an unknown value is shown as itself rather than
 * dropped — a console this client predates still reads as something.
 */
export const PLATFORMS: { key: string; label: string }[] = [
  { key: "nes", label: "NES" },
  { key: "snes", label: "SNES" },
  { key: "n64", label: "Nintendo 64" },
  { key: "gb", label: "Game Boy" },
  { key: "gbc", label: "Game Boy Color" },
  { key: "gba", label: "Game Boy Advance" },
  { key: "sms", label: "Master System" },
  { key: "genesis", label: "Genesis" },
  { key: "ps1", label: "PlayStation" },
];

export function platformLabel(key: string | null | undefined): string {
  if (!key) return "";
  return PLATFORMS.find((p) => p.key === key)?.label ?? key;
}

/** Orders consoles the way the filter shows them; unknown ones go last. */
export function orderPlatforms(keys: string[]): string[] {
  const rank = (k: string) => {
    const i = PLATFORMS.findIndex((p) => p.key === k);
    return i < 0 ? PLATFORMS.length : i;
  };
  return [...keys].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
}
