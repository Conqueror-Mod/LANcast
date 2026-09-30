import { AccountIcon } from "./LibraryIcon";

/*
 * The pictures an account can choose for itself (PUT /api/profile/avatar).
 *
 * Drawn here, in the rail's own line style, rather than uploaded: the server
 * stores only a key, so there is nothing to host and no way for one account to
 * put arbitrary images in front of everyone else. The keys must match
 * store.Avatars on the server.
 *
 * Line animals rather than faces or initials. They read at 18px in the rail,
 * they need no colour — gold is spent on focus and nothing else — and six
 * shapes this different are told apart at a glance.
 */
export const AVATARS = [
  { key: "fox", label: "Fox" },
  { key: "owl", label: "Owl" },
  { key: "cat", label: "Cat" },
  { key: "wolf", label: "Wolf" },
  { key: "bear", label: "Bear" },
  { key: "rabbit", label: "Rabbit" },
] as const;

const eye = (cx: number, cy: number) => (
  <circle cx={cx} cy={cy} r="0.9" fill="currentColor" stroke="none" />
);

const shapes: Record<string, React.ReactNode> = {
  fox: (
    <>
      {/* Wide and low: broad cheeks flaring out, short pointed chin. */}
      <path d="M3 4.5 7 7.5h6l4-3-.6 6.2L13 12.5 10 15.5l-3-3-3.4-1.8z" />
      <path d="M4.4 10.9 7 11.6M15.6 10.9 13 11.6" />
      {eye(8, 9.6)}
      {eye(12, 9.6)}
    </>
  ),
  owl: (
    <>
      <path d="M5 4.5 7 6h6l2-1.5V12a5 5 0 0 1-10 0z" />
      <circle cx="8" cy="9.5" r="1.7" />
      <circle cx="12" cy="9.5" r="1.7" />
      <path d="M10 11.5v1.6" />
    </>
  ),
  cat: (
    <>
      <path d="M4.5 9.5V3.5l3.2 2.8h4.6l3.2-2.8v6a5.5 5.5 0 0 1-11 0z" />
      <path d="M2.3 11.5h3M2.6 13.5l2.8-.8M14.7 11.5h3M17.4 13.5l-2.8-.8" />
      {eye(8, 10)}
      {eye(12, 10)}
    </>
  ),
  wolf: (
    <>
      {/* Narrow and long: tall ears, a muzzle running down to the nose. */}
      <path d="M6 2 8 6.5h4L14 2l1 6.5-2 2.5-1.2 6H8.2L7 11 5 8.5z" />
      <path d="M9.2 15.4h1.6" />
      {eye(8.3, 9)}
      {eye(11.7, 9)}
    </>
  ),
  bear: (
    <>
      <circle cx="5.2" cy="5.6" r="2" />
      <circle cx="14.8" cy="5.6" r="2" />
      <circle cx="10" cy="11" r="5.5" />
      <circle cx="10" cy="13" r="1.6" />
      {eye(8, 9.6)}
      {eye(12, 9.6)}
    </>
  ),
  rabbit: (
    <>
      <path d="M8 9.2C6.8 6 6.8 2.4 8.1 2.4s1.6 3.4 1.1 6.6" />
      <path d="M12 9.2c1.2-3.2 1.2-6.8-.1-6.8s-1.6 3.4-1.1 6.6" />
      <circle cx="10" cy="13.2" r="4.4" />
      {eye(8.4, 12.6)}
      {eye(11.6, 12.6)}
    </>
  ),
};

/**
 * An account's picture, or the plain person glyph when none is chosen — or
 * when the server names one this client does not know, since the set may grow.
 */
export function AvatarGlyph({
  avatar,
  size = 18,
}: {
  avatar?: string | null;
  size?: number;
}) {
  const shape = avatar ? shapes[avatar] : undefined;
  if (!shape) return <AccountIcon />;
  return (
    <svg
      className="rail-icon"
      viewBox="0 0 20 20"
      width={size}
      height={size}
      style={size !== 18 ? { width: size, height: size } : undefined}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      data-avatar={avatar}
    >
      {shape}
    </svg>
  );
}
